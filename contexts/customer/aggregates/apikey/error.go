package apikey

import (
	"fmt"
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 13

var (
	ErrorNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "api key not found"}
	// ErrorPermissionNotAllowed is a Permission no key may carry, whoever asks
	// (authz.KeyPermissions).
	ErrorPermissionNotAllowed = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "an api key cannot carry this permission"}
	// ErrorPermissionNotHeld keeps a key from exceeding the member who makes it.
	ErrorPermissionNotHeld = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "an api key cannot carry a permission your role lacks"}
	ErrorLimitReached      = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: fmt.Sprintf("a project holds at most %d active api keys", MaxActivePerProject)}
	ErrorCannotCreate      = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "api key could not be created"}
	ErrorCannotRevoke      = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "api key could not be revoked"}
)
