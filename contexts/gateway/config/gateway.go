// Package config is the gateway's route table: the authorization source of
// truth. A route absent from it does not exist.
package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

// TeamIDParam is the path parameter a guarded route must carry: the Role the
// gateway checks belongs to one Team, and the path says which (docs/adr/0005).
const TeamIDParam = "teamID"

type Endpoint struct {
	Path   string `json:"path" yaml:"path"`
	Method string `json:"method" yaml:"method"`
	// Public routes need no token. Permissions are the per-endpoint gate the
	// caller's Role in the path's Team must hold; an authenticated route with
	// none listed needs only a live session.
	Public      bool     `json:"public,omitempty" yaml:"public,omitempty"`
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	DoNotLog    bool     `json:"doNotLog,omitempty" yaml:"doNotLog,omitempty"`
	pathRegex   *regexp.Regexp
}

// Param reads the named :param out of a request path that matched this
// endpoint. The second result is false when the pattern has no such param or
// the path does not match.
func (e *Endpoint) Param(path, name string) (string, bool) {
	index := e.pathRegex.SubexpIndex(name)
	if index < 0 {
		return "", false
	}

	match := e.pathRegex.FindStringSubmatch(path)
	if match == nil {
		return "", false
	}

	return match[index], true
}

func (e *Endpoint) hasParam(name string) bool {
	return e.pathRegex.SubexpIndex(name) >= 0
}

type Endpoints []Endpoint

// Service is one upstream: the target-service header names it by its key
// under services, so the header value and the YAML key are the same word.
type Service struct {
	Host      string    `json:"host" yaml:"host"`
	Endpoints Endpoints `json:"endpoints" yaml:"endpoints"`
}

func (s Service) endpoint(path, method string, public bool) *Endpoint {
	for i := range s.Endpoints {
		e := &s.Endpoints[i]
		if e.Public == public && strings.EqualFold(e.Method, method) && e.pathRegex.MatchString(path) {
			return e
		}
	}

	return nil
}

// PublicEndpoint resolves a route that needs no token; a route listed for
// another service is not found here, whatever its path.
func (s Service) PublicEndpoint(path, method string) *Endpoint {
	return s.endpoint(path, method, true)
}

// PrivateEndpoint resolves a route that needs a live session.
func (s Service) PrivateEndpoint(path, method string) *Endpoint {
	return s.endpoint(path, method, false)
}

type Gateway struct {
	Name     string             `json:"name" yaml:"name"`
	Services map[string]Service `json:"services" yaml:"services"`
}

type GatewayConfig struct {
	Gateway Gateway `json:"gateway" yaml:"gateway"`
}

// Service looks a target up by the name the header carries. The lookup is
// exact: the table is deny-by-default, so a near miss is an unlisted target.
func (g *GatewayConfig) Service(name string) (Service, bool) {
	service, ok := g.Gateway.Services[name]

	return service, ok
}

var paramPattern = regexp.MustCompile(`:([a-zA-Z0-9_]+)`)

// CompileEndpointPathRegex turns an Echo-style path with :params into an
// anchored regex whose named groups carry the params; a bare wildcard matches
// everything.
func CompileEndpointPathRegex(path string) (*regexp.Regexp, error) {
	switch path {
	case "*", "/*", "**":
		return regexp.Compile(`^.*$`)
	}

	pathRegex := paramPattern.ReplaceAllString(regexp.QuoteMeta(path), `(?P<${1}>[^/]+)`)

	return regexp.Compile("^" + pathRegex + "$")
}

// Validate compiles every path and refuses a table that would guard a route
// wrongly: permissions on a path with no :teamID would never find a Role, and a
// permission no Role holds would lock everyone out. Both are caught before the
// first request rather than discovered by a client.
func (g *GatewayConfig) Validate() error {
	for name, service := range g.Gateway.Services {
		for i, endpoint := range service.Endpoints {
			if str.IsBlank(endpoint.Path) || str.IsBlank(endpoint.Method) {
				return fmt.Errorf("gateway service %q: path and method must be set for each endpoint", name)
			}

			re, err := CompileEndpointPathRegex(endpoint.Path)
			if err != nil {
				return fmt.Errorf("gateway endpoint %s %s: %w", endpoint.Method, endpoint.Path, err)
			}
			g.Gateway.Services[name].Endpoints[i].pathRegex = re

			if err := g.Gateway.Services[name].Endpoints[i].validatePermissions(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Endpoint) validatePermissions() error {
	if len(e.Permissions) == 0 {
		return nil
	}

	if !e.hasParam(TeamIDParam) {
		return fmt.Errorf("gateway endpoint %s %s: permissions need a :%s path parameter", e.Method, e.Path, TeamIDParam)
	}

	for _, permission := range e.Permissions {
		if !authz.IsPermission(permission) {
			return fmt.Errorf("gateway endpoint %s %s: unknown permission %q", e.Method, e.Path, permission)
		}
	}

	return nil
}

// InitEndpoints is Validate for the binary: a bad route table stops start-up.
func (g *GatewayConfig) InitEndpoints() {
	cobra.CheckErr(g.Validate())
}
