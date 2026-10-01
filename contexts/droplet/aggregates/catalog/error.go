package catalog

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 21

var (
	ErrorRegionNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "region not found"}
	ErrorPlanNotFound   = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "plan not found"}
	ErrorImageNotFound  = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "image not found"}
	// ErrorPlanSoldOut and ErrorPlanPriceChanged are answered at creation
	// time when the live Provider check disagrees with the catalog the customer
	// saw; the web app refreshes the catalog and explains.
	ErrorPlanSoldOut      = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "plan sold out"}
	ErrorPlanPriceChanged = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusConflict, Message: "plan price changed"}
	ErrorCannotSync       = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "catalog could not be synced"}
	ErrorCannotUpdate     = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "catalog could not be updated"}
)
