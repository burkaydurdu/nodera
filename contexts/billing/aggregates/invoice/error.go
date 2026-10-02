package invoice

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 33

var (
	// ErrorNotFound also answers another Team's Invoice.
	ErrorNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "invoice not found"}
	// ErrorNotPayable: the Invoice is paid or void (contract: 409 33-10002).
	ErrorNotPayable  = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "invoice is not payable"}
	ErrorCannotStore = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "invoice could not be stored"}
	ErrorCannotRead  = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "invoice could not be read"}
	// ErrorLinkInvalid answers a signed PDF link that is forged, for another
	// Invoice or past its expiry.
	ErrorLinkInvalid = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusGone, Message: "invoice download link is invalid or expired"}
	// ErrorPDFUnavailable: the renderer could not produce the PDF right now.
	ErrorPDFUnavailable = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusServiceUnavailable, Message: "invoice PDF is not available yet"}
	// ErrorPaymentInProgress: an attempt is still waiting for the provider or
	// for the cardholder's confirmation, so another would risk a second
	// charge.
	ErrorPaymentInProgress = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a payment is already in progress"}
)
