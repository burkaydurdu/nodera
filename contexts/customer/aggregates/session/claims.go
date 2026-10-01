package session

import "github.com/golang-jwt/jwt/v5"

// AccessClaims is what an access token carries. The gateway and the service
// both read it; `sid` is what ties the token to a revocable session, `teams`
// and `pv` let the gateway authorize without a database (docs/adr/0005).
type AccessClaims struct {
	jwt.RegisteredClaims
	Type      string            `json:"typ"`
	SessionID string            `json:"sid"`
	Email     string            `json:"email"`
	Status    string            `json:"status"`
	Teams     map[string]string `json:"teams"`
	PV        int64             `json:"pv"`
}

type RefreshClaims struct {
	jwt.RegisteredClaims
	Type      string `json:"typ"`
	SessionID string `json:"sid"`
}
