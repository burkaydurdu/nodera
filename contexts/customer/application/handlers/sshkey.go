package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey/publickey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/pkg/contextutil"
)

type SSHKey struct {
	teams team.IService
	keys  sshkey.IService
}

func NewSSHKey(teams team.IService, keys sshkey.IService) *SSHKey {
	return &SSHKey{teams: teams, keys: keys}
}

// Every route establishes membership first: the Team id is caller-controlled
// and a stranger must see the same not-found as for any other Team route.
func (h *SSHKey) List(ctx context.Context, req *sshkey.ListRequest) ([]*dto.SSHKey, error) {
	teamID, err := requireMembership(ctx, h.teams, req.TeamID)
	if err != nil {
		return nil, err
	}

	keys, err := h.keys.FindByTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}

	return dto.ToSSHKeys(keys), nil
}

func (h *SSHKey) Create(ctx context.Context, req *sshkey.CreateRequest) (*dto.SSHKey, error) {
	teamID, err := requireMembership(ctx, h.teams, req.TeamID)
	if err != nil {
		return nil, err
	}

	parsed, err := publickey.Parse(req.PublicKey)
	if err != nil {
		return nil, err
	}

	key := &sshkey.SSHKey{
		TeamID:      teamID,
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
	teamID, err := requireMembership(ctx, h.teams, req.TeamID)
	if err != nil {
		return err
	}

	return h.keys.Delete(ctx, teamID, uuid.MustParse(req.KeyID))
}
