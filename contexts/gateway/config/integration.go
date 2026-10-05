package config

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

// IntegrationGatewayConfig is the integration gateway's route table
// (docs/adr/0010). It reads the same YAML as the customer gateway's, but its
// callers are a Team's software holding an API key: they send no target
// header, so a route's method and path alone name its service, and every
// route is guarded by Permissions an API key can carry.
type IntegrationGatewayConfig struct {
	Gateway Gateway `json:"gateway" yaml:"gateway"`
}

// Route is where a request resolved to: the upstream service and the
// endpoint whose Permissions the API key must hold.
type Route struct {
	Service  string
	Host     string
	Endpoint *Endpoint
}

// Route finds the one listed route a request matches. Validate makes two
// services matching the same request impossible, so which service is looked
// at first cannot change the answer.
func (g *IntegrationGatewayConfig) Route(path, method string) (Route, bool) {
	for name, service := range g.Gateway.Services {
		for i := range service.Endpoints {
			endpoint := &service.Endpoints[i]
			if endpoint.matches(path, method) {
				return Route{Service: name, Host: service.Host, Endpoint: endpoint}, true
			}
		}
	}

	return Route{}, false
}

// Validate compiles every path and refuses a table the integration gateway
// could not route or guard: an upstream it could not reach, a route without
// Permissions (a key would pass on authentication alone), a Permission no
// API key can carry (no key could ever call the route), a public or
// headerless route (nothing is reachable without a key), and routes of two
// services that could match one request (the service would be left to
// chance).
func (g *IntegrationGatewayConfig) Validate() error {
	for _, name := range g.Gateway.serviceNames() {
		service := g.Gateway.Services[name]
		if err := validateUpstream(name, service.Host); err != nil {
			return err
		}

		for i := range service.Endpoints {
			endpoint := &service.Endpoints[i]
			if str.IsBlank(endpoint.Path) || str.IsBlank(endpoint.Method) {
				return fmt.Errorf("integration gateway service %q: path and method must be set for each endpoint", name)
			}

			re, err := CompileEndpointPathRegex(endpoint.Path)
			if err != nil {
				return fmt.Errorf("integration gateway endpoint %s %s: %w", endpoint.Method, endpoint.Path, err)
			}
			endpoint.pathRegex = re

			if endpoint.Public || endpoint.Headerless {
				return fmt.Errorf("integration gateway endpoint %s %s: every route needs an API key, so none may be public or headerless", endpoint.Method, endpoint.Path)
			}

			if err := endpoint.validateKeyPermissions(); err != nil {
				return err
			}
		}
	}

	return g.validateServicesAreUnambiguous()
}

// InitEndpoints is Validate for the binary: a bad route table stops start-up.
func (g *IntegrationGatewayConfig) InitEndpoints() {
	cobra.CheckErr(g.Validate())
}

func (g *IntegrationGatewayConfig) validateServicesAreUnambiguous() error {
	a, b, found := g.Gateway.ambiguousRoutes(func(*Endpoint) bool { return true })
	if !found {
		return nil
	}

	return fmt.Errorf("integration gateway endpoints %s %s (%s) and %s %s (%s): routes of two services can match the same request",
		a.endpoint.Method, a.endpoint.Path, a.service, b.endpoint.Method, b.endpoint.Path, b.service)
}

func (e *Endpoint) validateKeyPermissions() error {
	if len(e.Permissions) == 0 {
		return fmt.Errorf("integration gateway endpoint %s %s: permissions must be listed", e.Method, e.Path)
	}

	for _, permission := range e.Permissions {
		if !authz.IsPermission(permission) {
			return fmt.Errorf("integration gateway endpoint %s %s: unknown permission %q", e.Method, e.Path, permission)
		}
		if !authz.IsKeyPermission(permission) {
			return fmt.Errorf("integration gateway endpoint %s %s: permission %q is not one an API key can carry", e.Method, e.Path, permission)
		}
	}

	return nil
}

// validateUpstream catches a host the proxy could never reach at start-up,
// rather than as a 502 on every request.
func validateUpstream(service, host string) error {
	upstream, err := url.Parse(host)
	if err != nil || (upstream.Scheme != "http" && upstream.Scheme != "https") || str.IsEmpty(upstream.Host) {
		return fmt.Errorf("integration gateway service %q: host %q must be an http or https URL", service, host)
	}

	return nil
}
