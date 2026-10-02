package taxrule

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 35

var (
	// ErrorNoRule means the seed is missing its default row: an Invoice is
	// never issued with an unknown tax.
	ErrorNoRule     = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "no tax rule applies"}
	ErrorCannotRead = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "tax rules could not be read"}
)
