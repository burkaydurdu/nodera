package profile

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 30

var (
	ErrorNotFound    = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "billing profile not found"}
	ErrorCannotStore = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "billing profile could not be stored"}
	// ErrorCountryLocked: a card saved for one Payment provider must never be
	// charged by another, so the country stays until the cards are removed.
	ErrorCountryLocked         = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusConflict, Message: "remove the saved payment methods before changing the billing country"}
	ErrorInvalidTaxIdentifiers = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid tax identifiers for the country and account type"}
	ErrorCannotRead            = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "billing profile could not be read"}
)
