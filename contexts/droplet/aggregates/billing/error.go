package billing

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 24

var (
	ErrorCannotStore    = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "billing status could not be stored"}
	ErrorMalformedEvent = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed event payload"}
)
