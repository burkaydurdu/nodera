package session

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 2

var (
	ErrorInvalidRefreshToken = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "invalid or expired refresh token"}
	// ErrorRefreshTokenReused fires when a rotated-out refresh token comes back:
	// the session it belonged to has been revoked as a precaution.
	ErrorRefreshTokenReused = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "refresh token was already used; session revoked"}
	ErrorNotFound           = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "session not found"}
	ErrorCannotIssue        = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "session could not be created"}
)
