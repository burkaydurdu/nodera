package project

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 6

var (
	ErrorNotFound     = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "project not found"}
	ErrorNameTaken    = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a project with this name already exists in the team"}
	ErrorCannotCreate = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "project could not be created"}
)
