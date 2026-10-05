package apikey

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 58

var (
	ErrorUnauthorized = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "invalid api key"}
	ErrorForbidden    = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "the api key does not allow this"}
	ErrorUnavailable  = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusServiceUnavailable, Message: "api keys cannot be checked right now"}
)
