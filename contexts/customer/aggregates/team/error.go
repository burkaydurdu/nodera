package team

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 5

var (
	ErrorNotFound     = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "team not found"}
	ErrorCannotCreate = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "team could not be created"}
	// ErrorNotMember is a 404, not a 403: the caller must not learn that a
	// Team they are not in exists.
	ErrorNotMember = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "team not found"}
)
