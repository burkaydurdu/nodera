package limits

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 54

var (
	// ErrorPaused lasts until an operator lifts the Pause.
	ErrorPaused = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusLocked, Message: "sending is paused for this project"}
	// ErrorDailyLimitReached lasts until the next UTC midnight.
	ErrorDailyLimitReached = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "the daily limit is reached"}
	ErrorCannotStore       = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "limits could not be stored"}
	ErrorNotFound          = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "no limits recorded for this project"}
)
