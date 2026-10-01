package otp

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 4

var (
	ErrorInvalidCode       = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "invalid or expired code"}
	ErrorAttemptsExhausted = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "too many wrong codes, request a new one"}
	ErrorCooldown          = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "a code was sent recently, wait before requesting another"}
)
