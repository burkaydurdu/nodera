// Package config is the gateway's route table: the authorization source of
// truth. A route absent from it does not exist.
package config

import (
	"fmt"
	"regexp"
	"sort"
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
	Public bool `json:"public,omitempty" yaml:"public,omitempty"`
	// Headerless routes are reached without the target-service header, for
	// callers the gateway does not control (payment-provider webhooks, the
	// card issuer's 3-D Secure return). Such a caller cannot send a token
	// either, so only a public route may be headerless.
	Headerless  bool     `json:"headerless,omitempty" yaml:"headerless,omitempty"`
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

func (e *Endpoint) matches(path, method string) bool {
	return strings.EqualFold(e.Method, method) && e.pathRegex.MatchString(path)
}

// canMatchTheSameRequest compares the patterns segment by segment: a segment
// holding a :param matches any one segment, so it never tells two routes
// apart. A segment mixing text and a param counts as a param too, erring
// towards a refused start over an ambiguous route.
func (e *Endpoint) canMatchTheSameRequest(other *Endpoint) bool {
	if !strings.EqualFold(e.Method, other.Method) {
		return false
	}
	if isWildcard(e.Path) || isWildcard(other.Path) {
		return true
	}

	a, b := strings.Split(e.Path, "/"), strings.Split(other.Path, "/")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] && !paramPattern.MatchString(a[i]) && !paramPattern.MatchString(b[i]) {
			return false
		}
	}

	return true
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
		if e.Public == public && e.matches(path, method) {
			return e
		}
	}

	return nil
}

func (s Service) listsHeaderless(path, method string) bool {
	for i := range s.Endpoints {
		e := &s.Endpoints[i]
		if e.Headerless && e.Public && e.matches(path, method) {
			return true
		}
	}

	return false
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

// HeaderlessService names the service a request without the target header
// belongs to. Only headerless routes are consulted, and only one service may
// answer: Validate makes a second impossible, and the lookup still refuses
// to guess between two.
func (g *GatewayConfig) HeaderlessService(path, method string) (string, bool) {
	var found []string
	for name, service := range g.Gateway.Services {
		if service.listsHeaderless(path, method) {
			found = append(found, name)
		}
	}

	if len(found) != 1 {
		return "", false
	}

	return found[0], true
}

var paramPattern = regexp.MustCompile(`:([a-zA-Z0-9_]+)`)

// CompileEndpointPathRegex turns an Echo-style path with :params into an
// anchored regex whose named groups carry the params; a bare wildcard matches
// everything.
func CompileEndpointPathRegex(path string) (*regexp.Regexp, error) {
	if isWildcard(path) {
		return regexp.Compile(`^.*$`)
	}

	pathRegex := paramPattern.ReplaceAllString(regexp.QuoteMeta(path), `(?P<${1}>[^/]+)`)

	return regexp.Compile("^" + pathRegex + "$")
}

func isWildcard(path string) bool {
	switch path {
	case "*", "/*", "**":
		return true
	}

	return false
}

// Validate compiles every path and refuses a table that would route or guard
// a request wrongly: permissions on a path with no :teamID would never find a
// Role, a permission no Role holds would lock everyone out, a headerless
// private route could never be authenticated, and two services claiming one
// headerless request would leave the target to chance. All are caught before
// the first request rather than discovered by a client.
func (g *GatewayConfig) Validate() error {
	for _, name := range g.serviceNames() {
		service := g.Gateway.Services[name]
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

			if endpoint.Headerless && !endpoint.Public {
				return fmt.Errorf("gateway endpoint %s %s: headerless routes must be public", endpoint.Method, endpoint.Path)
			}
		}
	}

	return g.validateHeaderlessAreUnambiguous()
}

// serviceNames orders the services so a table with several faults always
// reports the same one first.
func (g *GatewayConfig) serviceNames() []string {
	names := make([]string, 0, len(g.Gateway.Services))
	for name := range g.Gateway.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func (g *GatewayConfig) validateHeaderlessAreUnambiguous() error {
	type route struct {
		service  string
		endpoint *Endpoint
	}

	var routes []route
	for _, name := range g.serviceNames() {
		endpoints := g.Gateway.Services[name].Endpoints
		for i := range endpoints {
			if endpoints[i].Headerless {
				routes = append(routes, route{service: name, endpoint: &endpoints[i]})
			}
		}
	}

	for i := range routes {
		for j := i + 1; j < len(routes); j++ {
			a, b := routes[i], routes[j]
			if a.service != b.service && a.endpoint.canMatchTheSameRequest(b.endpoint) {
				return fmt.Errorf("gateway endpoints %s %s (%s) and %s %s (%s): headerless routes in two services can match the same request",
					a.endpoint.Method, a.endpoint.Path, a.service, b.endpoint.Method, b.endpoint.Path, b.service)
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
