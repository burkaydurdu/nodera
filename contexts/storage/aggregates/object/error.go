package object

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 65

var (
	ErrorInvalidCursor = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid page cursor"}
	ErrorInvalidKey    = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid object key"}
	ErrorNotFound      = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "object not found"}
)
