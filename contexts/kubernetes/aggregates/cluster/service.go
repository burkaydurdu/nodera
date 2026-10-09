package cluster

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/cluster.go -package=mockservices -mock_names=IService=MockIClusterService github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster IService
type IService interface {
	Create(ctx context.Context, c *Cluster) error
	Find(ctx context.Context, projectID, id uuid.UUID) (*Cluster, error)
	FindForUpdate(ctx context.Context, projectID, id uuid.UUID) (*Cluster, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Cluster, error)
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Cluster, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Cluster, error)
	CountByProject(ctx context.Context, projectID uuid.UUID) (int, error)
	NextEndpointPort(ctx context.Context, region string, first, last int) (int, error)
	ListLiveByTeam(ctx context.Context, teamID uuid.UUID) ([]*Cluster, error)
	ListByStatus(ctx context.Context, statuses []Status, limit int) ([]*Cluster, error)
	Save(ctx context.Context, c *Cluster) error
	SetTeamSuspended(ctx context.Context, teamID uuid.UUID, suspended bool) ([]uuid.UUID, error)

	CreatePool(ctx context.Context, p *NodePool) error
	FindPool(ctx context.Context, clusterID, id uuid.UUID) (*NodePool, error)
	FindPoolForUpdate(ctx context.Context, clusterID, id uuid.UUID) (*NodePool, error)
	ListPools(ctx context.Context, clusterID uuid.UUID) ([]*NodePool, error)
	SavePool(ctx context.Context, p *NodePool) error

	CreateNode(ctx context.Context, n *Node) error
	FindNodeByID(ctx context.Context, id uuid.UUID) (*Node, error)
	FindNodeByIDForUpdate(ctx context.Context, id uuid.UUID) (*Node, error)
	ListNodes(ctx context.Context, clusterID uuid.UUID) ([]*Node, error)
	ListNodesByPool(ctx context.Context, poolID uuid.UUID) ([]*Node, error)
	ListNodesByStatus(ctx context.Context, statuses []NodeStatus, limit int) ([]*Node, error)
	ListNodesAwaitingRequest(ctx context.Context, limit int) ([]*Node, error)
	SaveNode(ctx context.Context, n *Node) error
}

type IRepository interface {
	IService
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, c *Cluster) error { return s.repository.Create(ctx, c) }

func (s *Service) Find(ctx context.Context, projectID, id uuid.UUID) (*Cluster, error) {
	return s.repository.Find(ctx, projectID, id)
}

func (s *Service) FindForUpdate(ctx context.Context, projectID, id uuid.UUID) (*Cluster, error) {
	return s.repository.FindForUpdate(ctx, projectID, id)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*Cluster, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Cluster, error) {
	return s.repository.FindByIDForUpdate(ctx, id)
}

func (s *Service) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Cluster, error) {
	return s.repository.ListByProject(ctx, projectID)
}

func (s *Service) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	return s.repository.CountByProject(ctx, projectID)
}

func (s *Service) NextEndpointPort(ctx context.Context, region string, first, last int) (int, error) {
	return s.repository.NextEndpointPort(ctx, region, first, last)
}

func (s *Service) ListLiveByTeam(ctx context.Context, teamID uuid.UUID) ([]*Cluster, error) {
	return s.repository.ListLiveByTeam(ctx, teamID)
}

func (s *Service) ListByStatus(ctx context.Context, statuses []Status, limit int) ([]*Cluster, error) {
	return s.repository.ListByStatus(ctx, statuses, limit)
}

func (s *Service) Save(ctx context.Context, c *Cluster) error { return s.repository.Save(ctx, c) }

func (s *Service) SetTeamSuspended(ctx context.Context, teamID uuid.UUID, suspended bool) ([]uuid.UUID, error) {
	return s.repository.SetTeamSuspended(ctx, teamID, suspended)
}

func (s *Service) CreatePool(ctx context.Context, p *NodePool) error {
	return s.repository.CreatePool(ctx, p)
}

func (s *Service) FindPool(ctx context.Context, clusterID, id uuid.UUID) (*NodePool, error) {
	return s.repository.FindPool(ctx, clusterID, id)
}

func (s *Service) FindPoolForUpdate(ctx context.Context, clusterID, id uuid.UUID) (*NodePool, error) {
	return s.repository.FindPoolForUpdate(ctx, clusterID, id)
}

func (s *Service) ListPools(ctx context.Context, clusterID uuid.UUID) ([]*NodePool, error) {
	return s.repository.ListPools(ctx, clusterID)
}

func (s *Service) SavePool(ctx context.Context, p *NodePool) error {
	return s.repository.SavePool(ctx, p)
}

func (s *Service) CreateNode(ctx context.Context, n *Node) error {
	return s.repository.CreateNode(ctx, n)
}

func (s *Service) FindNodeByID(ctx context.Context, id uuid.UUID) (*Node, error) {
	return s.repository.FindNodeByID(ctx, id)
}

func (s *Service) FindNodeByIDForUpdate(ctx context.Context, id uuid.UUID) (*Node, error) {
	return s.repository.FindNodeByIDForUpdate(ctx, id)
}

func (s *Service) ListNodes(ctx context.Context, clusterID uuid.UUID) ([]*Node, error) {
	return s.repository.ListNodes(ctx, clusterID)
}

func (s *Service) ListNodesByPool(ctx context.Context, poolID uuid.UUID) ([]*Node, error) {
	return s.repository.ListNodesByPool(ctx, poolID)
}

func (s *Service) ListNodesByStatus(ctx context.Context, statuses []NodeStatus, limit int) ([]*Node, error) {
	return s.repository.ListNodesByStatus(ctx, statuses, limit)
}

func (s *Service) ListNodesAwaitingRequest(ctx context.Context, limit int) ([]*Node, error) {
	return s.repository.ListNodesAwaitingRequest(ctx, limit)
}

func (s *Service) SaveNode(ctx context.Context, n *Node) error { return s.repository.SaveNode(ctx, n) }
