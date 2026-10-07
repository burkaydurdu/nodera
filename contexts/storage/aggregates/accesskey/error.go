package accesskey

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 64

var (
	ErrorNotFound    = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "access key not found"}
	ErrorTooMany     = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the storage holds as many access keys as it may"}
	ErrorCannotStore = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "access key could not be stored"}
)
