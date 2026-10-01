package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey/publickey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

type SSHKey struct {
	teams team.IService
	keys  sshkey.IService
}

func NewSSHKey(teams team.IService, keys sshkey.IService) *SSHKey {
	return &SSHKey{teams: teams, keys: keys}
}

// Every route establishes the caller's Role first: the Team id is
// caller-controlled and a stranger must see the same not-found as for any
// other Team route.
func (h *SSHKey) List(ctx context.Context, req *sshkey.ListRequest) ([]*dto.SSHKey, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.TeamRead)
	if err != nil {
		return nil, err
	}

	keys, err := h.keys.FindByTeamID(ctx, m.Team.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToSSHKeys(keys), nil
}

func (h *SSHKey) Create(ctx context.Context, req *sshkey.CreateRequest) (*dto.SSHKey, error) {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.SSHKeyWrite)
	if err != nil {
		return nil, err
	}

	parsed, err := publickey.Parse(req.PublicKey)
	if err != nil {
		return nil, err
	}

	key := &sshkey.SSHKey{
		TeamID:      m.Team.ID,
		Name:        req.Name,
		PublicKey:   parsed.PublicKey,
		Fingerprint: parsed.Fingerprint,
		CreatedBy:   contextutil.CurrentUserID(ctx),
	}
	if err := h.keys.Create(ctx, key); err != nil {
		return nil, err
	}

	return dto.ToSSHKey(key), nil
}

func (h *SSHKey) Delete(ctx context.Context, req *sshkey.DeleteRequest) error {
	m, err := requirePermission(ctx, h.teams, req.TeamID, authz.SSHKeyWrite)
	if err != nil {
		return err
	}

	return h.keys.Delete(ctx, m.Team.ID, uuid.MustParse(req.KeyID))
}
