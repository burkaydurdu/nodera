package storage

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 63

var (
	// ErrorNotFound answers an unknown Storage and one of another Project
	// alike.
	ErrorNotFound           = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "storage not found"}
	ErrorInvalidBucketName  = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a bucket name is 3 to 63 lower-case letters, digits and hyphens, and starts and ends with a letter or a digit"}
	ErrorReservedBucketName = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "this bucket name is reserved"}
	ErrorBucketNameTaken    = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "this bucket name is already taken in the region"}
	ErrorTooManyStorages    = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the project holds as many storages as it may"}
	// ErrorDoesNotFit is a smaller Package than what the Storage holds.
	ErrorDoesNotFit = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the storage holds more than this package"}
	// ErrorNameMismatch is a deletion that did not repeat the Bucket name.
	ErrorNameMismatch = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the bucket name does not match the storage"}
	ErrorPaused       = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusLocked, Message: "the storage is paused"}
	ErrorDeleting     = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the storage is being deleted"}
	// ErrorUnavailable is the storage servers not answering; nothing was
	// changed and the request can be repeated.
	ErrorUnavailable   = rerror.Error{Code: 10010, Domain: Domain, HttpStatus: http.StatusServiceUnavailable, Message: "storage is temporarily unavailable"}
	ErrorCannotStore   = rerror.Error{Code: 10011, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "storage could not be stored"}
	ErrorInvalidReason = rerror.Error{Code: 10012, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "unknown pause reason"}
	// ErrorEngineRefused is the storage servers answering no to something
	// Nodera asked; repeating the request will not help, an operator must look.
	ErrorEngineRefused = rerror.Error{Code: 10013, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "storage refused the request"}
)
