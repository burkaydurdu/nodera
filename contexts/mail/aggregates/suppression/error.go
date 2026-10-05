package suppression

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 56

var (
	ErrorNotFound      = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "suppression not found"}
	ErrorComplaintKept = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a complaint suppression can only be removed by Nodera"}
	ErrorCannotStore   = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "suppression could not be stored"}
	ErrorInvalidCursor = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid cursor"}
)
