// Package apperror holds the errors that belong to no aggregate: the generic
// envelope for panics and unknown failures, and the context-invariant misses
// that mean broken middleware wiring.
package apperror

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 100

var (
	ErrorGeneric = rerror.Error{Code: -1, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "error"}
	ErrorPanic   = rerror.Error{Code: -2, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "panic"}
	ErrorCommon  = rerror.Error{Code: 10000, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "unexpected error"}

	ErrorMissingCorrelationID = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "correlation id is missing"}
	ErrorMissingUserID        = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "user id is missing"}
	ErrorMissingSessionID     = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "session id is missing"}
	ErrorRateLimited          = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "too many requests"}
)
