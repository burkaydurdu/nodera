package cluster

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const (
	nameIndex         = "clusters_name_idx"
	endpointPortIndex = "clusters_endpoint_port_idx"
	poolNameIndex     = "node_pools_name_idx"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// ---------------------------------------------------------------------------
// Clusters
// ---------------------------------------------------------------------------

const clusterColumns = `id, project_id, team_id, region, name, version, status, endpoint, endpoint_port, team_suspended, failure_code, failure_reason,
	control_plane_created_at, control_plane_ready_at, prepared_at, control_plane_deleted_at,
	created_by, created_at, updated_at, deleted_at`

// shown are the Clusters a member sees; a fully deleted one is not there for
// anybody.
const shown = `status <> 'deleted'`

// Create inserts the Cluster; the unique index settles two requests for one
// name in a Project.
func (r *Repository) Create(ctx context.Context, c *Cluster) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO clusters (id, project_id, team_id, region, name, version, status, endpoint, endpoint_port, team_suspended, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)`,
		c.ID, c.ProjectID, c.TeamID, c.Region, c.Name, c.Version, string(c.Status), c.Endpoint, c.EndpointPort, c.TeamSuspended, c.CreatedBy, c.CreatedAt)
	if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == nameIndex {
		return ErrorNameTaken
	}
	if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == endpointPortIndex {
		return ErrorNoEndpointPort.Copy().SetError(err)
	}
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	c.UpdatedAt = c.CreatedAt

	return nil
}

func (r *Repository) Find(ctx context.Context, projectID, id uuid.UUID) (*Cluster, error) {
	return r.one(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE id = $1 AND project_id = $2 AND `+shown, id, projectID)
}

func (r *Repository) FindForUpdate(ctx context.Context, projectID, id uuid.UUID) (*Cluster, error) {
	return r.one(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE id = $1 AND project_id = $2 AND `+shown+` FOR UPDATE`, id, projectID)
}

// FindByID is for the jobs and the consumers, who come with a Cluster and no
// Project; a deleted Cluster is still found, so a late event lands harmlessly.
func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*Cluster, error) {
	return r.one(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE id = $1`, id)
}

func (r *Repository) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Cluster, error) {
	return r.one(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE id = $1 FOR UPDATE`, id)
}

func (r *Repository) one(ctx context.Context, query string, args ...any) (*Cluster, error) {
	c, err := scanCluster(r.queries.Executor(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return c, err
}

func (r *Repository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Cluster, error) {
	return r.list(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE project_id = $1 AND `+shown+` ORDER BY created_at, id`, projectID)
}

// CountByProject counts every Cluster still holding a name, so one being
// deleted keeps its place until it is gone.
func (r *Repository) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT count(*) FROM clusters WHERE project_id = $1 AND `+shown, projectID).Scan(&n)

	return n, err
}

// NextEndpointPort answers the lowest port p of the range first..last, both
// included, such that no Cluster of the Region holds p or p+1, the port after
// each Endpoint's being its tunnel's. It takes a lock that holds the Region's
// other openings until the caller's transaction ends, so two openings never
// answer one port; the unique index is the backstop.
func (r *Repository) NextEndpointPort(ctx context.Context, region string, first, last int) (int, error) {
	if _, err := r.queries.Executor(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('clusters_endpoint_port:' || $1))`, region); err != nil {
		return 0, ErrorCannotStore.Copy().SetError(err)
	}
	var port int
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT p FROM generate_series($2::int, $3::int - 1, 2) AS p
		WHERE NOT EXISTS (SELECT 1 FROM clusters WHERE region = $1 AND endpoint_port IN (p, p + 1) AND `+shown+`)
		ORDER BY p
		LIMIT 1`, region, first, last).Scan(&port)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrorNoEndpointPort
	}
	if err != nil {
		return 0, ErrorCannotStore.Copy().SetError(err)
	}

	return port, nil
}

// ListLiveByTeam is every Cluster of the Team that may still be deleted.
func (r *Repository) ListLiveByTeam(ctx context.Context, teamID uuid.UUID) ([]*Cluster, error) {
	return r.list(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE team_id = $1 AND status NOT IN ('deleting', 'deleted') ORDER BY created_at, id`, teamID)
}

// ListByStatus pages the Clusters in any of the statuses, oldest first, for
// the jobs that work on them.
func (r *Repository) ListByStatus(ctx context.Context, statuses []Status, limit int) ([]*Cluster, error) {
	values := make([]string, 0, len(statuses))
	for _, s := range statuses {
		values = append(values, string(s))
	}

	return r.list(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE status = ANY($1) ORDER BY updated_at, id LIMIT $2`, values, limit)
}

// Save writes everything about a Cluster that changes after it is opened. The
// caller holds the row (FindForUpdate) so that two changes never overwrite
// each other.
func (r *Repository) Save(ctx context.Context, c *Cluster) error {
	var code *string
	if c.FailureCode != nil {
		s := string(*c.FailureCode)
		code = &s
	}
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE clusters
		SET name = $2, status = $3, endpoint = $4, team_suspended = $5, failure_code = $6, failure_reason = $7,
		    control_plane_created_at = $8, control_plane_ready_at = $9, prepared_at = $10, control_plane_deleted_at = $11,
		    deleted_at = $12, updated_at = $13
		WHERE id = $1`,
		c.ID, c.Name, string(c.Status), c.Endpoint, c.TeamSuspended, code, c.FailureReason,
		c.ControlPlaneCreatedAt, c.ControlPlaneReadyAt, c.PreparedAt, c.ControlPlaneDeletedAt, c.DeletedAt, c.UpdatedAt)
	if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == nameIndex {
		return ErrorNameTaken
	}
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// SetTeamSuspended carries the Team's Billing status onto its live Clusters
// and answers the ones it changed.
func (r *Repository) SetTeamSuspended(ctx context.Context, teamID uuid.UUID, suspended bool) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		UPDATE clusters SET team_suspended = $2, updated_at = now()
		WHERE team_id = $1 AND status NOT IN ('deleting', 'deleted') AND team_suspended <> $2
		RETURNING id`, teamID, suspended)
	if err != nil {
		return nil, ErrorCannotStore.Copy().SetError(err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}

	return out, rows.Err()
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]*Cluster, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Cluster{}
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}

	return out, rows.Err()
}

func scanCluster(row pgx.Row) (*Cluster, error) {
	var c Cluster
	var status string
	var code *string
	err := row.Scan(&c.ID, &c.ProjectID, &c.TeamID, &c.Region, &c.Name, &c.Version, &status, &c.Endpoint, &c.EndpointPort, &c.TeamSuspended, &code, &c.FailureReason,
		&c.ControlPlaneCreatedAt, &c.ControlPlaneReadyAt, &c.PreparedAt, &c.ControlPlaneDeletedAt,
		&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.DeletedAt)
	if err != nil {
		return nil, err
	}
	c.Status = Status(status)
	if code != nil {
		fc := FailureCode(*code)
		c.FailureCode = &fc
	}

	return &c, nil
}

// ---------------------------------------------------------------------------
// Node pools
// ---------------------------------------------------------------------------

const poolColumns = `id, cluster_id, name, plan_code, count, labels, taints, created_at, updated_at, deleted_at`

func (r *Repository) CreatePool(ctx context.Context, p *NodePool) error {
	labels, taints, err := encodePool(p)
	if err != nil {
		return err
	}
	_, err = r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO node_pools (id, cluster_id, name, plan_code, count, labels, taints, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		p.ID, p.ClusterID, p.Name, p.PlanCode, p.Count, labels, taints, p.CreatedAt)
	if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == poolNameIndex {
		return ErrorPoolNameTaken
	}
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	p.UpdatedAt = p.CreatedAt

	return nil
}

func (r *Repository) FindPool(ctx context.Context, clusterID, id uuid.UUID) (*NodePool, error) {
	return r.onePool(ctx, `SELECT `+poolColumns+` FROM node_pools WHERE id = $1 AND cluster_id = $2 AND deleted_at IS NULL`, id, clusterID)
}

func (r *Repository) FindPoolForUpdate(ctx context.Context, clusterID, id uuid.UUID) (*NodePool, error) {
	return r.onePool(ctx, `SELECT `+poolColumns+` FROM node_pools WHERE id = $1 AND cluster_id = $2 AND deleted_at IS NULL FOR UPDATE`, id, clusterID)
}

func (r *Repository) onePool(ctx context.Context, query string, args ...any) (*NodePool, error) {
	p, err := scanPool(r.queries.Executor(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorPoolNotFound
	}

	return p, err
}

// ListPools is the Cluster's Node pools that exist, oldest first.
func (r *Repository) ListPools(ctx context.Context, clusterID uuid.UUID) ([]*NodePool, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `SELECT `+poolColumns+` FROM node_pools WHERE cluster_id = $1 AND deleted_at IS NULL ORDER BY created_at, id`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*NodePool{}
	for rows.Next() {
		p, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}

	return out, rows.Err()
}

func (r *Repository) SavePool(ctx context.Context, p *NodePool) error {
	labels, taints, err := encodePool(p)
	if err != nil {
		return err
	}
	_, err = r.queries.Executor(ctx).Exec(ctx, `
		UPDATE node_pools SET count = $2, labels = $3, taints = $4, deleted_at = $5, updated_at = $6 WHERE id = $1`,
		p.ID, p.Count, labels, taints, p.DeletedAt, p.UpdatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func encodePool(p *NodePool) ([]byte, []byte, error) {
	if p.Labels == nil {
		p.Labels = map[string]string{}
	}
	if p.Taints == nil {
		p.Taints = []Taint{}
	}
	labels, err := json.Marshal(p.Labels)
	if err != nil {
		return nil, nil, ErrorCannotStore.Copy().SetError(err)
	}
	taints, err := json.Marshal(p.Taints)
	if err != nil {
		return nil, nil, ErrorCannotStore.Copy().SetError(err)
	}

	return labels, taints, nil
}

func scanPool(row pgx.Row) (*NodePool, error) {
	var p NodePool
	var labels, taints []byte
	if err := row.Scan(&p.ID, &p.ClusterID, &p.Name, &p.PlanCode, &p.Count, &labels, &taints, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt); err != nil {
		return nil, err
	}
	p.Labels = map[string]string{}
	p.Taints = []Taint{}
	if len(labels) > 0 {
		if err := json.Unmarshal(labels, &p.Labels); err != nil {
			return nil, err
		}
	}
	if len(taints) > 0 {
		if err := json.Unmarshal(taints, &p.Taints); err != nil {
			return nil, err
		}
	}

	return &p, nil
}

// ---------------------------------------------------------------------------
// Nodes
// ---------------------------------------------------------------------------

const nodeColumns = `id, cluster_id, pool_id, hostname, status, droplet_id, droplet_status, ipv4, failure_code, failure_reason,
	requested_at, droplet_active_at, ready_at, release_asked_at, released_at, gone_at, created_at, updated_at`

func (r *Repository) CreateNode(ctx context.Context, n *Node) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO nodes (id, cluster_id, pool_id, hostname, status, droplet_status, requested_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, '', $6, $7, $7)`,
		n.ID, n.ClusterID, n.PoolID, n.Hostname, string(n.Status), n.RequestedAt, n.CreatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	n.UpdatedAt = n.CreatedAt

	return nil
}

func (r *Repository) FindNodeByID(ctx context.Context, id uuid.UUID) (*Node, error) {
	return r.oneNode(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = $1`, id)
}

func (r *Repository) FindNodeByIDForUpdate(ctx context.Context, id uuid.UUID) (*Node, error) {
	return r.oneNode(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = $1 FOR UPDATE`, id)
}

func (r *Repository) oneNode(ctx context.Context, query string, args ...any) (*Node, error) {
	n, err := scanNode(r.queries.Executor(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound.Copy().SetMessage("node not found")
	}

	return n, err
}

// ListNodes is every Node of the Cluster still shown, oldest first.
func (r *Repository) ListNodes(ctx context.Context, clusterID uuid.UUID) ([]*Node, error) {
	return r.listNodes(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE cluster_id = $1 AND status <> 'gone' ORDER BY created_at, id`, clusterID)
}

func (r *Repository) ListNodesByPool(ctx context.Context, poolID uuid.UUID) ([]*Node, error) {
	return r.listNodes(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE pool_id = $1 AND status <> 'gone' ORDER BY created_at, id`, poolID)
}

// ListNodesByStatus pages the Nodes in any of the statuses, oldest first.
func (r *Repository) ListNodesByStatus(ctx context.Context, statuses []NodeStatus, limit int) ([]*Node, error) {
	values := make([]string, 0, len(statuses))
	for _, s := range statuses {
		values = append(values, string(s))
	}

	return r.listNodes(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE status = ANY($1) ORDER BY updated_at, id LIMIT $2`, values, limit)
}

// ListNodesAwaitingRequest is the Nodes not yet asked of the droplet context
// whose Cluster's Control plane is ready to be joined.
func (r *Repository) ListNodesAwaitingRequest(ctx context.Context, limit int) ([]*Node, error) {
	return r.listNodes(ctx, `
		SELECT `+nodeColumns+` FROM nodes n
		WHERE n.status = 'requested' AND n.requested_at IS NULL
		  AND EXISTS (SELECT 1 FROM clusters c WHERE c.id = n.cluster_id AND c.control_plane_ready_at IS NOT NULL AND c.prepared_at IS NOT NULL
		              AND c.status NOT IN ('deleting', 'deleted', 'failed'))
		ORDER BY n.created_at, n.id LIMIT $1`, limit)
}

func (r *Repository) SaveNode(ctx context.Context, n *Node) error {
	var code *string
	if n.FailureCode != nil {
		s := string(*n.FailureCode)
		code = &s
	}
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE nodes
		SET status = $2, droplet_id = $3, droplet_status = $4, ipv4 = $5, failure_code = $6, failure_reason = $7, requested_at = $8,
		    droplet_active_at = $9, ready_at = $10, release_asked_at = $11, released_at = $12, gone_at = $13, updated_at = $14
		WHERE id = $1`,
		n.ID, string(n.Status), n.DropletID, n.DropletStatus, n.IPv4, code, n.FailureReason, n.RequestedAt,
		n.DropletActiveAt, n.ReadyAt, n.ReleaseAskedAt, n.ReleasedAt, n.GoneAt, n.UpdatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) listNodes(ctx context.Context, query string, args ...any) ([]*Node, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}

	return out, rows.Err()
}

func scanNode(row pgx.Row) (*Node, error) {
	var n Node
	var status string
	var code *string
	err := row.Scan(&n.ID, &n.ClusterID, &n.PoolID, &n.Hostname, &status, &n.DropletID, &n.DropletStatus, &n.IPv4, &code, &n.FailureReason,
		&n.RequestedAt, &n.DropletActiveAt, &n.ReadyAt, &n.ReleaseAskedAt, &n.ReleasedAt, &n.GoneAt, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	n.Status = NodeStatus(status)
	if code != nil {
		fc := FailureCode(*code)
		n.FailureCode = &fc
	}

	return &n, nil
}
