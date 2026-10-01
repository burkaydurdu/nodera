package access

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 23

var (
	// ErrorProjectNotFound answers an unknown Project and a Project of another
	// Team alike, so Project ids cannot be enumerated.
	ErrorProjectNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "project not found"}
	ErrorCannotStore     = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "project access could not be stored"}
	ErrorMalformedEvent  = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed event payload"}
)
