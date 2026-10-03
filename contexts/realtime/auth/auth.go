// Package auth decides who is on the other end of a socket, from the same
// access token and Redis keys the gateway checks (docs/adr/0002,
// docs/adr/0005). A socket outlives its token, so the check is repeated on
// every heartbeat rather than trusted from the handshake.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
	"github.com/piggystats/nodera/utils/str"
)

// The Redis keys the customer service writes; wire contracts, declared here
// rather than imported.
const (
	SessionKeyFormat     = "session:%s"
	PermVersionKeyFormat = "user_perm_version:%s"
)

// ErrUnauthorized is any reason the caller is not, or no longer, who the
// token says: the web app refreshes its token and reconnects.
var ErrUnauthorized = errors.New("unauthorized")

// Identity is who a socket belongs to and what their token allowed when it
// was last presented.
type Identity struct {
	UserID      uuid.UUID
	SessionID   uuid.UUID
	Teams       map[uuid.UUID]authz.Role
	PermVersion int64
}

// Accepts reports whether a message for the address may reach this socket.
func (i *Identity) Accepts(address realtime.Address) bool {
	switch {
	case address.UserID != nil:
		return *address.UserID == i.UserID
	case address.TeamID != nil:
		role, ok := i.Teams[*address.TeamID]

		return ok && authz.Allows(role, address.Permission)
	default:
		return false
	}
}

type Cache interface {
	Exists(ctx context.Context, key string) (bool, error)
	// GetString answers "" for an absent key.
	GetString(ctx context.Context, key string) (string, error)
}

type Authenticator struct {
	secret []byte
	cache  Cache
}

func NewAuthenticator(secret string, cache Cache) *Authenticator {
	return &Authenticator{secret: []byte(secret), cache: cache}
}

// Verify reads a presented access token and checks it is still current.
func (a *Authenticator) Verify(ctx context.Context, raw string) (*Identity, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}

		return a.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrUnauthorized
	}
	if typ, _ := claims["typ"].(string); typ != "access" {
		return nil, ErrUnauthorized
	}

	identity, ok := identityOf(claims)
	if !ok {
		return nil, ErrUnauthorized
	}
	if err := a.Check(ctx, identity); err != nil {
		return nil, err
	}

	return identity, nil
}

// Check answers ErrUnauthorized when the session was signed out or a
// membership changed since the token was issued; any other error is Redis
// being unreachable, which says nothing about the caller.
func (a *Authenticator) Check(ctx context.Context, identity *Identity) error {
	alive, err := a.cache.Exists(ctx, fmt.Sprintf(SessionKeyFormat, identity.SessionID))
	if err != nil {
		return err
	}
	if !alive {
		return ErrUnauthorized
	}

	value, err := a.cache.GetString(ctx, fmt.Sprintf(PermVersionKeyFormat, identity.UserID))
	if err != nil {
		return err
	}
	// An absent key is version 0, what a User with no membership change has.
	current := int64(0)
	if str.IsNotEmpty(value) {
		if current, err = strconv.ParseInt(value, 10, 64); err != nil {
			return err
		}
	}
	if current != identity.PermVersion {
		return ErrUnauthorized
	}

	return nil
}

func identityOf(claims jwt.MapClaims) (*Identity, bool) {
	subject, err := claims.GetSubject()
	if err != nil {
		return nil, false
	}
	userID, err := uuid.Parse(subject)
	if err != nil {
		return nil, false
	}
	rawSession, _ := claims["sid"].(string)
	sessionID, err := uuid.Parse(rawSession)
	if err != nil {
		return nil, false
	}

	// Tokens issued before docs/adr/0005 carry neither claim; they are stale
	// by definition and refresh once.
	rawTeams, ok := claims["teams"].(map[string]any)
	if !ok {
		return nil, false
	}
	teams := make(map[uuid.UUID]authz.Role, len(rawTeams))
	for rawID, value := range rawTeams {
		teamID, err := uuid.Parse(rawID)
		role, isString := value.(string)
		if err != nil || !isString {
			return nil, false
		}
		teams[teamID] = authz.Role(role)
	}

	permVersion, ok := wholeNumber(claims["pv"])
	if !ok {
		return nil, false
	}

	return &Identity{UserID: userID, SessionID: sessionID, Teams: teams, PermVersion: permVersion}, true
}

func wholeNumber(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}

		return int64(v), true
	case json.Number:
		n, err := v.Int64()

		return n, err == nil
	default:
		return 0, false
	}
}
