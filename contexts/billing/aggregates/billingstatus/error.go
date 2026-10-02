package billingstatus

import (
	"net/http"

	"github.com/piggystats/rerror"
)

// Domain 38 follows the contract's 30–37 block; Billing status is its own
// aggregate because Payment methods and collection both move it.
const Domain rerror.Domain = 38

var (
	ErrorCannotStore = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "billing status could not be stored"}
	ErrorCannotRead  = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "billing status could not be read"}
)
