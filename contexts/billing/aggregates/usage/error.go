package usage

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 32

var (
	ErrorCannotStore = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "usage could not be stored"}
	ErrorCannotRead  = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "usage could not be read"}
	// ErrorMalformedEvent is terminal: redelivering the same payload cannot fix it.
	ErrorMalformedEvent = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed usage event"}
)
