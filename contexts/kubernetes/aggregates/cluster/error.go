package cluster

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 73

var (
	// ErrorNotFound answers an unknown Cluster and one of another Project alike.
	ErrorNotFound          = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "cluster not found"}
	ErrorInvalidName       = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a name is 1 to 63 lower-case letters, digits and hyphens, and starts and ends with a letter or a digit"}
	ErrorNameTaken         = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a cluster with this name already exists in the project"}
	ErrorTooManyClusters   = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the project holds as many clusters as it may"}
	ErrorPoolNotFound      = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "node pool not found"}
	ErrorInvalidPoolName   = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a node pool name is 1 to 32 lower-case letters, digits and hyphens, and starts and ends with a letter or a digit"}
	ErrorPoolNameTaken     = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a node pool with this name already exists in the cluster"}
	ErrorTooManyPools      = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the cluster holds as many node pools as it may"}
	ErrorTooManyNodes      = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "a node pool holds between 1 and the allowed number of nodes"}
	ErrorLastPool          = rerror.Error{Code: 10010, Domain: Domain, HttpStatus: http.StatusConflict, Message: "a cluster keeps at least one node pool"}
	ErrorInvalidLabel      = rerror.Error{Code: 10011, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid label"}
	ErrorInvalidTaint      = rerror.Error{Code: 10012, Domain: Domain, HttpStatus: http.StatusBadRequest, Message: "invalid taint"}
	ErrorDeleting          = rerror.Error{Code: 10013, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the cluster is being deleted"}
	ErrorNotRunning        = rerror.Error{Code: 10014, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the cluster's control plane is not ready yet"}
	ErrorInvalidTransition = rerror.Error{Code: 10015, Domain: Domain, HttpStatus: http.StatusConflict, Message: "invalid status transition"}
	ErrorCannotStore       = rerror.Error{Code: 10016, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "cluster could not be stored"}
	// ErrorUnavailable is the control plane servers not answering; nothing
	// was changed and the request can be repeated.
	ErrorUnavailable = rerror.Error{Code: 10017, Domain: Domain, HttpStatus: http.StatusServiceUnavailable, Message: "kubernetes is temporarily unavailable"}
	// ErrorEngineRefused is the control plane servers answering no to
	// something Nodera asked; repeating will not help, an operator must look.
	ErrorEngineRefused = rerror.Error{Code: 10018, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "kubernetes refused the request"}
	ErrorFailed        = rerror.Error{Code: 10019, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the cluster has failed; delete it and open a new one"}
	// ErrorControlPlaneTimedOut is the reason a Control plane that never
	// became ready is failed with.
	ErrorControlPlaneTimedOut = rerror.Error{Code: 10020, Domain: Domain, HttpStatus: http.StatusGatewayTimeout, Message: "the control plane was not ready in time"}
	// ErrorNodeTimedOut is the reason a Node that never joined is failed with.
	ErrorNodeTimedOut = rerror.Error{Code: 10021, Domain: Domain, HttpStatus: http.StatusGatewayTimeout, Message: "the node did not join the cluster in time"}
	// ErrorNoEndpointPort is a Region whose every Endpoint port is taken; an
	// operator widens the range or a Cluster is deleted first.
	ErrorNoEndpointPort = rerror.Error{Code: 10022, Domain: Domain, HttpStatus: http.StatusServiceUnavailable, Message: "the region has no room for another cluster"}
)
