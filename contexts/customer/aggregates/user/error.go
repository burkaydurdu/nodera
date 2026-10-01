package user

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 1

var (
	// ErrorNotFound answers a wrong email and a wrong password alike, so the
	// sign-in form cannot be used to discover who has an account.
	ErrorNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "user not found"}

	ErrorEmailNotVerified = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "email address is not verified"}
	ErrorSuspended        = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "account is suspended"}
	ErrorAlreadyVerified  = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "email address is already verified"}

	// ErrorAccountLocked carries "retry_after_seconds" in its fields; the API
	// layer turns it into the Retry-After header.
	ErrorAccountLocked = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "account temporarily locked due to too many failed sign-in attempts"}

	ErrorIncorrectPassword     = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "current password is incorrect"}
	ErrorInvalidOrExpiredToken = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid or expired password reset token"}
	ErrorCannotCreate          = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "user could not be created"}
	ErrorCannotUpdate          = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "user could not be updated"}
	ErrorMFAMethodAlreadySet   = rerror.Error{Code: 10010, Domain: Domain, HttpStatus: http.StatusConflict, Message: "mfa method is already set"}
	// ErrorEmailTaken never reaches a caller: sign-up turns it into the same
	// 201 a fresh address gets.
	ErrorEmailTaken = rerror.Error{Code: 10011, Domain: Domain, HttpStatus: http.StatusConflict, Message: "email is already registered"}
)
