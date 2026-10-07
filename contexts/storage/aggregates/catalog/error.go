package catalog

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 62

var (
	ErrorUnknownRegion  = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "unknown region"}
	ErrorUnknownPackage = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "unknown package"}
)
