// Package config is the gateway's route table: the authorization source of
// truth. A route absent from it does not exist.
package config

import (
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

type Endpoint struct {
	Path   string `json:"path" yaml:"path"`
	Method string `json:"method" yaml:"method"`
	// Public routes need no token. Permissions is the per-endpoint gate for a
	// later phase; an authenticated route with none listed needs only a live
	// session.
	Public      bool     `json:"public,omitempty" yaml:"public,omitempty"`
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	DoNotLog    bool     `json:"doNotLog,omitempty" yaml:"doNotLog,omitempty"`
	pathRegex   *regexp.Regexp
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
// anchored regex; a bare wildcard matches everything.
func CompileEndpointPathRegex(path string) (*regexp.Regexp, error) {
	switch path {
	case "*", "/*", "**":
		return regexp.Compile(`^.*$`)
	}

	pathRegex := paramPattern.ReplaceAllString(regexp.QuoteMeta(path), `[^/]+`)

	return regexp.Compile("^" + pathRegex + "$")
}

func (g *GatewayConfig) InitEndpoints() {
	for name, service := range g.Gateway.Services {
		for i, endpoint := range service.Endpoints {
			if endpoint.Path == "" || endpoint.Method == "" {
				cobra.CheckErr("path and method must be set for each gateway endpoint")
			}

			re, err := CompileEndpointPathRegex(endpoint.Path)
			cobra.CheckErr(err)
			g.Gateway.Services[name].Endpoints[i].pathRegex = re
		}
	}
}
