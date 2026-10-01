package invitation

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 8

var (
	ErrorNotFound = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "invitation not found"}
	// ErrorNoLongerValid covers accepted, declined, revoked and expired alike:
	// the link is dead and only a new Invitation helps.
	ErrorNoLongerValid   = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusGone, Message: "invitation is no longer valid"}
	ErrorForAnotherEmail = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "invitation is for another email address"}
	ErrorAlreadyMember   = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "this email address already belongs to a member of the team"}
	ErrorAlreadyPending  = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusConflict, Message: "this email address already has a pending invitation to the team"}
	// ErrorResendCooldown carries "retry_after_seconds"; the API layer turns
	// it into the Retry-After header.
	ErrorResendCooldown = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "invitation was sent a moment ago; try again shortly"}
	ErrorEmailMismatch  = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invitation is for a different email address than the one signing up"}
	ErrorCannotCreate   = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "invitation could not be created"}
	ErrorCannotUpdate   = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "invitation could not be updated"}
)
