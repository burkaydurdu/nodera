package billing

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 71

var (
	ErrorCannotStore    = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "billing status could not be stored"}
	ErrorMalformedEvent = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed event payload"}
	// The Team's Billing status, as for Droplets: no card yet, or suspended
	// for an unpaid Invoice.
	ErrorPaymentMethodRequired = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusPaymentRequired, Message: "payment method required"}
	ErrorTeamSuspended         = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusLocked, Message: "team suspended"}
)
