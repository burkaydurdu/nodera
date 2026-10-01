package session

import "github.com/golang-jwt/jwt/v5"

// AccessClaims is what an access token carries. The gateway and the service
// both read it; `sid` is what ties the token to a revocable session.
type AccessClaims struct {
	jwt.RegisteredClaims
	Type      string `json:"typ"`
	SessionID string `json:"sid"`
	Email     string `json:"email"`
	Status    string `json:"status"`
}

type RefreshClaims struct {
	jwt.RegisteredClaims
	Type      string `json:"typ"`
	SessionID string `json:"sid"`
}
