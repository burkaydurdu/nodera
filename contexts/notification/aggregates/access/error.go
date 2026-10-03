package access

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 41

var (
	ErrorCannotStore    = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "team access could not be stored"}
	ErrorCannotRead     = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "team access could not be read"}
	ErrorMalformedEvent = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed event payload"}
)
