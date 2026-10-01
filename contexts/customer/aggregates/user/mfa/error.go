package mfa

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 3

var (
	ErrorUnknownMethod      = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "unknown mfa method"}
	ErrorInvalidChallenge   = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "invalid or expired login challenge"}
	ErrorResendNotSupported = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "resend is not supported for this mfa method"}
)
