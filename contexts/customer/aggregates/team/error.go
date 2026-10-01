package team

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 5

var (
	ErrorNotFound     = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "team not found"}
	ErrorCannotCreate = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "team could not be created"}
	// ErrorNotMember is a 404, not a 403: the caller must not learn that a
	// Team they are not in exists.
	ErrorNotMember = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "team not found"}
	// ErrorForbidden is for a member: they know the Team exists, so the honest
	// answer is which door is closed.
	ErrorForbidden      = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusForbidden, Message: "not allowed for your role"}
	ErrorLastOwner      = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a team must keep at least one owner"}
	ErrorOwnRole        = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusConflict, Message: "you cannot change your own role"}
	ErrorMemberNotFound = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "member not found"}
	ErrorCannotUpdate   = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "team could not be updated"}
)
