package ceph

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/engine"
)

// CreateAccount finds or makes the Project's account, named after the Project,
// and its root user, whose uid is the Project id. The root user is made with
// one key, and that key is answered on every call, so a second call after a
// crash between the two steps finishes what the first began.
func (c *Client) CreateAccount(ctx context.Context, projectID uuid.UUID) (*engine.Account, error) {
	name := projectID.String()
	account, err := c.admin.getAccountByName(ctx, name)
	if err != nil {
		if !adminNotFound(err) {
			return nil, wrap("looking up the Project's account", err)
		}
		account, err = c.admin.createAccount(ctx, name)
		// Two first Storages of one Project opened at once both get here; the
		// one that lost the race reads what the other made.
		if adminConflict(err) {
			account, err = c.admin.getAccountByName(ctx, name)
		}
		if err != nil {
			return nil, wrap("creating the Project's account", err)
		}
	}

	root, err := c.admin.getUser(ctx, name)
	if err != nil {
		if !adminNotFound(err) {
			return nil, wrap("looking up the Project's root user", err)
		}
		root, err = c.admin.createAccountRoot(ctx, name, rootDisplayName, account.ID)
		if adminConflict(err) {
			root, err = c.admin.getUser(ctx, name)
		}
		if err != nil {
			return nil, wrap("creating the Project's root user", err)
		}
	}
	if root.AccountID != account.ID {
		return nil, fmt.Errorf("the Project's root user belongs to account %s, not %s", root.AccountID, account.ID)
	}
	if len(root.Keys) == 0 {
		return nil, errors.New("the Project's root user has no key")
	}

	return &engine.Account{ID: account.ID, AccessKey: root.Keys[0].AccessKey, SecretKey: root.Keys[0].SecretKey}, nil
}
