package catalog

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 72

var (
	ErrorRegionNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "region not found"}
	ErrorPlanNotFound   = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "plan not found"}
	// ErrorPlanNotForNodes is a Plan the catalog sells but too small to be a Node.
	ErrorPlanNotForNodes = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a node needs a plan with at least 2 cpus and 4 gb of ram"}
	ErrorPlanSoldOut     = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "plan sold out"}
	// ErrorRegionNotSold is a Region of the catalog where Nodera runs no
	// control plane fleet yet.
	ErrorRegionNotSold      = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "kubernetes is not sold in this region"}
	ErrorVersionNotOffered  = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "this kubernetes version is not offered"}
	ErrorCannotStore        = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "catalog could not be stored"}
	ErrorMalformedEvent     = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed event payload"}
	ErrorNoVersionAvailable = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusServiceUnavailable, Message: "no kubernetes version is offered right now"}
)
