package threshold

import (
	"net/http"

	"github.com/piggystats/rerror"
)

// Domain 39 follows billingstatus's 38; the contract's 30–37 block names no
// Threshold domain.
const Domain rerror.Domain = 39

var (
	// ErrorMissing means the seed lacks the kind's row.
	ErrorMissing    = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "no threshold configured"}
	ErrorCannotRead = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "thresholds could not be read"}
)
