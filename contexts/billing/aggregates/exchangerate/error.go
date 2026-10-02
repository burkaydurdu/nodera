package exchangerate

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 36

var (
	// ErrorNoRate: no rate on or before the day; an Invoice for a Turkish Team
	// cannot be issued without one.
	ErrorNoRate      = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "no exchange rate available"}
	ErrorCannotStore = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "exchange rate could not be stored"}
	ErrorFetchFailed = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadGateway, Message: "exchange rate could not be fetched"}
	ErrorCannotRead  = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "exchange rates could not be read"}
)
