package message

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 55

var (
	ErrorNotFound          = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "message not found"}
	ErrorNoRecipients      = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a message needs at least one recipient"}
	ErrorTooManyRecipients = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a message holds at most 50 recipients"}
	ErrorInvalidAddress    = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "not an email address"}
	ErrorTooLarge          = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a message is at most 10 MB"}
	ErrorNoContent         = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a message needs a text or an html body"}
	ErrorReservedHeader    = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "this header is set by Nodera"}
	ErrorInvalidAttachment = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "attachment content is not base64"}
	// ErrorSenderNotVerified is a sender outside the Project's verified
	// Sending domains: nothing is sent.
	ErrorSenderNotVerified = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusUnprocessableEntity, Message: "the sender is not on a verified sending domain of this project"}
	ErrorInvalidCursor     = rerror.Error{Code: 10010, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid cursor"}
	ErrorCannotStore       = rerror.Error{Code: 10011, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "message could not be stored"}
	ErrorInvalidTag        = rerror.Error{Code: 10012, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "at most 10 tags of at most 64 characters"}
	ErrorMalformedMessage  = rerror.Error{Code: 10013, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "the message could not be read"}
	// ErrorMalformedRecords answers a post of the MTA's log hook that cannot
	// be read; a 4xx makes the hook drop it rather than retry it forever.
	ErrorMalformedRecords = rerror.Error{Code: 10014, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "not a log record"}
)
