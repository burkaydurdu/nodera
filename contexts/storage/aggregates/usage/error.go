package usage

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 66

var ErrorCannotStore = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "usage could not be stored"}
