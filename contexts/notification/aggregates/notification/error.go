package notification

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 40

var (
	// ErrorNotFound answers another User's Notification and one the caller can
	// no longer see alike, so ids cannot be probed.
	ErrorNotFound       = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "notification not found"}
	ErrorCannotStore    = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "notification could not be stored"}
	ErrorCannotRead     = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "notification could not be read"}
	ErrorMalformedEvent = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "malformed event payload"}
	ErrorUnknownType    = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "unknown notification type"}
	ErrorNoSender       = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "no sender for the channel"}
)
