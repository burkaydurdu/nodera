package sendingdomain

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 52

const MaxPerProject = 10

var (
	ErrorNotFound       = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "sending domain not found"}
	ErrorInvalidName    = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "not a domain that can send mail"}
	ErrorAlreadyAdded   = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusConflict, Message: "sending domain already added to this project"}
	ErrorTooMany        = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a project holds at most 10 sending domains"}
	ErrorVerifyTooSoon  = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "checked less than a minute ago"}
	ErrorCannotStore    = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "sending domain could not be stored"}
	ErrorTokenCollision = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "sending domain token already taken"}
	ErrorKeyNotFound    = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "dkim key not found"}
)
