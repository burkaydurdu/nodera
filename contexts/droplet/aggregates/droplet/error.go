package droplet

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 22

var (
	// ErrorNotFound also answers a Droplet of a Team the caller is not in.
	ErrorNotFound          = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "droplet not found"}
	ErrorActionNotAllowed  = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "action not allowed in the droplet's current status"}
	ErrorInvalidSSHKey     = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid ssh key"}
	ErrorCannotCreate      = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "droplet could not be created"}
	ErrorCannotUpdate      = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "droplet could not be updated"}
	ErrorInvalidTransition = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusConflict, Message: "invalid status transition"}
	// ErrorProvisioningTimedOut is the reason a Droplet the Provider never
	// delivered is failed with.
	ErrorProvisioningTimedOut = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusGatewayTimeout, Message: "the server was not delivered in time"}
)
