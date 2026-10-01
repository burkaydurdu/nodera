package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
	"github.com/piggystats/nodera/contexts/customer/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
)

type ISSHKeyHandler interface {
	List(ctx context.Context, req *sshkey.ListRequest) ([]*dto.SSHKey, error)
	Create(ctx context.Context, req *sshkey.CreateRequest) (*dto.SSHKey, error)
	Delete(ctx context.Context, req *sshkey.DeleteRequest) error
}

type SSHKey struct {
	handler      ISSHKeyHandler
	authenticate echo.MiddlewareFunc
}

func NewSSHKey(handler ISSHKeyHandler, authenticate echo.MiddlewareFunc) *SSHKey {
	return &SSHKey{handler: handler, authenticate: authenticate}
}

func (k *SSHKey) SetRoutes(group *echo.Group) {
	keys := group.Group("/teams/:teamID/ssh-keys", k.authenticate)
	keys.GET("", k.List)
	keys.POST("", k.Create)
	keys.DELETE("/:keyID", k.Delete)
}

// List godoc
// @Summary The SSH keys of a Team the caller belongs to
// @Tags ssh-keys
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {array} dto.SSHKey
// @Router /v1/teams/{teamID}/ssh-keys [get]
func (k *SSHKey) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sshkey.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	keys, err := k.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, keys)
}

// Create godoc
// @Summary Add an SSH public key to a Team the caller belongs to
// @Description Accepts one OpenSSH authorized_keys line of type ssh-ed25519 or ssh-rsa (at least 2048 bits).
// @Tags ssh-keys
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param request body sshkey.CreateRequest true "SSH key"
// @Success 201 {object} dto.SSHKey
// @Router /v1/teams/{teamID}/ssh-keys [post]
func (k *SSHKey) Create(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sshkey.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	created, err := k.handler.Create(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, created)
}

// Delete godoc
// @Summary Remove an SSH key from a Team the caller belongs to
// @Tags ssh-keys
// @Security BearerAuth
// @Param teamID path string true "Team id"
// @Param keyID path string true "SSH key id"
// @Success 204
// @Router /v1/teams/{teamID}/ssh-keys/{keyID} [delete]
func (k *SSHKey) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sshkey.DeleteRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := k.handler.Delete(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
