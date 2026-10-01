package sshkey

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 7

var (
	ErrorNotFound           = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "ssh key not found"}
	ErrorNameTaken          = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "an ssh key with this name already exists in the team"}
	ErrorFingerprintTaken   = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusConflict, Message: "this ssh key is already in the team"}
	ErrorInvalidPublicKey   = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "public key is not a valid OpenSSH public key"}
	ErrorUnsupportedKeyType = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "only ssh-ed25519 and ssh-rsa keys are accepted"}
	ErrorKeyTooShort        = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "ssh-rsa keys must be at least 2048 bits"}
	ErrorCannotCreate       = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "ssh key could not be created"}
	ErrorCannotDelete       = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "ssh key could not be deleted"}
)
