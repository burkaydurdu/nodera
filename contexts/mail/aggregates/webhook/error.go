package webhook

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 57

var (
	ErrorNotFound      = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "webhook not found"}
	ErrorTooMany       = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a project holds at most 3 webhooks"}
	ErrorInvalidURL    = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a webhook needs a public https url"}
	ErrorInvalidEvents = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "unknown message event"}
	ErrorCannotStore   = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "webhook could not be stored"}
	// ErrorPrivateTarget is a host that resolved to a private address at
	// delivery time; the attempt fails like any other.
	ErrorPrivateTarget = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusBadGateway, Message: "the webhook resolves to a private address"}
	ErrorInvalidCursor = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid cursor"}
)
