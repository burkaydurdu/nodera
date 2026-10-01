package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/piggystats/nodera/utils/str"
)

type Cache interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
	SetAdd(ctx context.Context, key string, expiration time.Duration, members ...string) error
	SetRemove(ctx context.Context, key string, members ...string) error
	SetMembers(ctx context.Context, key string) ([]string, error)
}

type Config struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Issuer     string
}

//go:generate mockgen -destination=../../mock/services/session.go -package=mockservices -mock_names=IManager=MockISessionManager github.com/piggystats/nodera/contexts/customer/aggregates/session IManager
type IManager interface {
	Issue(ctx context.Context, principal Principal, device Device) (*TokenPair, error)
	Refresh(ctx context.Context, refreshToken string, device Device, lookup PrincipalLookup) (*TokenPair, error)
	Exists(ctx context.Context, sessionID uuid.UUID) (bool, error)
	List(ctx context.Context, userID uuid.UUID) (Sessions, error)
	Revoke(ctx context.Context, userID, sessionID uuid.UUID) error
	RevokeAll(ctx context.Context, userID uuid.UUID) error
	RevokeAllExcept(ctx context.Context, userID, keep uuid.UUID) error
}

// PrincipalLookup lets Refresh re-read the User so a suspended account stops
// refreshing even though its refresh token is still valid.
type PrincipalLookup func(ctx context.Context, userID uuid.UUID) (*Principal, error)

type Manager struct {
	cache  Cache
	config Config
}

func NewManager(cache Cache, config Config) *Manager {
	return &Manager{cache: cache, config: config}
}

func (m *Manager) Issue(ctx context.Context, principal Principal, device Device) (*TokenPair, error) {
	sessionID, err := uuid.NewV7()
	if err != nil {
		return nil, ErrorCannotIssue.Copy().SetError(err)
	}

	now := time.Now()
	s := &Session{
		ID:         sessionID,
		UserID:     principal.UserID,
		IP:         device.IP,
		UserAgent:  device.UserAgent,
		CreatedAt:  now,
		LastUsedAt: now,
		ExpiresAt:  now.Add(m.config.RefreshTTL),
	}

	return m.rotate(ctx, s, principal)
}

// Refresh rotates the refresh token. A token whose jti is not the session's
// current one has been used before: the whole session is revoked, and the
// caller on the copied device is signed out with it.
func (m *Manager) Refresh(ctx context.Context, refreshToken string, device Device, lookup PrincipalLookup) (*TokenPair, error) {
	claims, err := m.parseRefresh(refreshToken)
	if err != nil {
		return nil, ErrorInvalidRefreshToken
	}

	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return nil, ErrorInvalidRefreshToken
	}

	s, err := m.get(ctx, sessionID)
	if err != nil {
		return nil, ErrorInvalidRefreshToken
	}

	if !s.acceptsJTI(claims.ID) {
		_ = m.Revoke(ctx, s.UserID, s.ID)

		return nil, ErrorRefreshTokenReused
	}

	principal, err := lookup(ctx, s.UserID)
	if err != nil {
		return nil, err
	}

	s.IP = device.IP
	s.UserAgent = device.UserAgent
	s.LastUsedAt = time.Now()

	return m.rotate(ctx, s, *principal)
}

func (m *Manager) Exists(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	raw, err := m.cache.GetString(ctx, fmt.Sprintf(KeyFormat, sessionID))
	if err != nil {
		return false, err
	}

	return str.IsNotEmpty(raw), nil
}

// List answers the live sessions of a User, pruning index entries whose
// session record has expired.
func (m *Manager) List(ctx context.Context, userID uuid.UUID) (Sessions, error) {
	ids, err := m.cache.SetMembers(ctx, fmt.Sprintf(UserIndexFormat, userID))
	if err != nil {
		return nil, err
	}

	sessions := make(Sessions, 0, len(ids))
	var stale []string
	for _, id := range ids {
		sessionID, err := uuid.Parse(id)
		if err != nil {
			stale = append(stale, id)
			continue
		}

		s, err := m.get(ctx, sessionID)
		if err != nil {
			stale = append(stale, id)
			continue
		}
		sessions = append(sessions, s)
	}

	if len(stale) > 0 {
		_ = m.cache.SetRemove(ctx, fmt.Sprintf(UserIndexFormat, userID), stale...)
	}

	return sessions, nil
}

func (m *Manager) Revoke(ctx context.Context, userID, sessionID uuid.UUID) error {
	if err := m.cache.Delete(ctx, fmt.Sprintf(KeyFormat, sessionID)); err != nil {
		return err
	}

	return m.cache.SetRemove(ctx, fmt.Sprintf(UserIndexFormat, userID), sessionID.String())
}

func (m *Manager) RevokeAll(ctx context.Context, userID uuid.UUID) error {
	return m.RevokeAllExcept(ctx, userID, uuid.Nil)
}

func (m *Manager) RevokeAllExcept(ctx context.Context, userID, keep uuid.UUID) error {
	ids, err := m.cache.SetMembers(ctx, fmt.Sprintf(UserIndexFormat, userID))
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(ids))
	members := make([]string, 0, len(ids))
	for _, id := range ids {
		if keep != uuid.Nil && id == keep.String() {
			continue
		}
		keys = append(keys, fmt.Sprintf(KeyFormat, id))
		members = append(members, id)
	}

	if len(keys) == 0 {
		return nil
	}
	if err := m.cache.Delete(ctx, keys...); err != nil {
		return err
	}

	return m.cache.SetRemove(ctx, fmt.Sprintf(UserIndexFormat, userID), members...)
}

// rotate mints a fresh refresh jti, persists the session under it and signs
// both tokens. It is the one place tokens are created.
func (m *Manager) rotate(ctx context.Context, s *Session, principal Principal) (*TokenPair, error) {
	jti, err := uuid.NewV7()
	if err != nil {
		return nil, ErrorCannotIssue.Copy().SetError(err)
	}
	s.PreviousJTI = s.RefreshJTI
	s.RefreshJTI = jti
	s.RotatedAt = time.Now()

	payload, err := json.Marshal(s)
	if err != nil {
		return nil, ErrorCannotIssue.Copy().SetError(err)
	}

	ttl := time.Until(s.ExpiresAt)
	if ttl <= 0 {
		return nil, ErrorInvalidRefreshToken
	}

	if err := m.cache.Set(ctx, fmt.Sprintf(KeyFormat, s.ID), string(payload), ttl); err != nil {
		return nil, ErrorCannotIssue.Copy().SetError(err)
	}
	if err := m.cache.SetAdd(ctx, fmt.Sprintf(UserIndexFormat, s.UserID), m.config.RefreshTTL, s.ID.String()); err != nil {
		return nil, ErrorCannotIssue.Copy().SetError(err)
	}

	now := time.Now()
	accessExpiresAt := now.Add(m.config.AccessTTL)
	access := jwt.NewWithClaims(jwt.SigningMethodHS256, AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.config.Issuer,
			Subject:   principal.UserID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExpiresAt),
		},
		Type:      TokenTypeAccess,
		SessionID: s.ID.String(),
		Email:     principal.Email,
		Status:    principal.Status,
		Teams:     teamsClaim(principal.Teams),
		PV:        principal.PermissionVersion,
	})
	accessToken, err := access.SignedString([]byte(m.config.Secret))
	if err != nil {
		return nil, ErrorCannotIssue.Copy().SetError(err)
	}

	refresh := jwt.NewWithClaims(jwt.SigningMethodHS256, RefreshClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.config.Issuer,
			Subject:   principal.UserID.String(),
			ID:        jti.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(s.ExpiresAt),
		},
		Type:      TokenTypeRefresh,
		SessionID: s.ID.String(),
	})
	refreshToken, err := refresh.SignedString([]byte(m.config.Secret))
	if err != nil {
		return nil, ErrorCannotIssue.Copy().SetError(err)
	}

	return &TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		AccessExpiresAt:  accessExpiresAt,
		RefreshExpiresAt: s.ExpiresAt,
	}, nil
}

// teamsClaim keeps the claim an object, never null: the gateway treats a
// token without it as stale.
func teamsClaim(teams map[string]string) map[string]string {
	if teams == nil {
		return map[string]string{}
	}

	return teams
}

// acceptsJTI is true for the current refresh token, or for the one it
// replaced while the rotation grace still runs.
func (s *Session) acceptsJTI(jti string) bool {
	if s.RefreshJTI.String() == jti {
		return true
	}

	return s.PreviousJTI != uuid.Nil && s.PreviousJTI.String() == jti && time.Since(s.RotatedAt) < RotationGrace
}

func (m *Manager) get(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	raw, err := m.cache.GetString(ctx, fmt.Sprintf(KeyFormat, sessionID))
	if err != nil {
		return nil, err
	}
	if str.IsEmpty(raw) {
		return nil, ErrorNotFound
	}

	var s Session
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, ErrorNotFound
	}

	return &s, nil
}

func (m *Manager) parseRefresh(token string) (*RefreshClaims, error) {
	var claims RefreshClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}

		return []byte(m.config.Secret), nil
	})
	if err != nil || !parsed.Valid || claims.Type != TokenTypeRefresh || str.IsEmpty(claims.ID) {
		return nil, ErrorInvalidRefreshToken
	}

	return &claims, nil
}
