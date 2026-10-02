package access

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 37

var (
	// ErrorTeamNotFound answers a stranger and an unknown Team alike, so Team
	// ids cannot be enumerated through the billing routes.
	ErrorTeamNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "team not found"}
	// ErrorForbidden is for a member of the Team whose Role lacks the
	// Permission (docs/adr/0005).
	ErrorForbidden      = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "not allowed for your role"}
	ErrorCannotStore    = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "team access could not be stored"}
	ErrorMalformedEvent = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed event payload"}
	ErrorCannotRead     = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "team access could not be read"}
)
