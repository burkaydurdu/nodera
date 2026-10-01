package provider

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 20

// Messages never name the Provider: the customer knows Nodera only.
var (
	ErrorNotFound      = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "provider not found"}
	ErrorNotConfigured = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "provider is not configured"}
	// ErrorUnavailable is a transport or upstream failure: the request may be
	// retried as-is.
	ErrorUnavailable = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadGateway, Message: "the server could not be requested right now"}
	// ErrorRejected is a refusal the upstream explained (balance, quota, an
	// invalid combination); the raw message is logged, never answered.
	ErrorRejected       = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadGateway, Message: "the server request was refused"}
	ErrorServerNotFound = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "server not found"}
	ErrorInvalidRef     = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "invalid server reference"}
)
