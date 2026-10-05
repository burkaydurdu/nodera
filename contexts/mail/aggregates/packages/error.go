package packages

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 53

var (
	ErrorUnknownPackage = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "unknown package"}
	// ErrorNoPackage is Mail not switched on for the Project.
	ErrorNoPackage = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusPaymentRequired, Message: "the project has no mail package"}
	// ErrorPackageExhausted answers until an upgrade or the next Period.
	ErrorPackageExhausted = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "the package's monthly allowance is used up"}
	ErrorCannotStore      = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "package could not be stored"}
	// ErrorNotEnoughAllowance is a Message with more recipients than the
	// Period has left; smaller ones still go, so it says how many are left.
	ErrorNotEnoughAllowance = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "the package has fewer emails left this period than the message has recipients"}
)
