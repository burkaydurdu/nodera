package config

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func routeTable() *GatewayConfig {
	g := &GatewayConfig{Gateway: Gateway{Services: map[string]Service{
		"customer": {Host: "http://customer:9650", Endpoints: Endpoints{
			{Path: "/v1/auth/login", Method: "POST", Public: true},
			{Path: "/v1/users/me", Method: "GET"},
			{Path: "/v1/users/me/sessions/:sessionID", Method: "DELETE"},
			{Path: "/v1/teams/:teamID/projects", Method: "POST"},
		}},
		"droplet": {Host: "http://droplet:9660", Endpoints: Endpoints{
			{Path: "/v1/catalog/regions", Method: "GET"},
			{Path: "/v1/projects/:projectID/droplets", Method: "POST"},
		}},
	}}}
	g.InitEndpoints()

	return g
}

func TestEndpoints_PathParamsMatchOneSegment(t *testing.T) {
	customer, ok := routeTable().Service("customer")
	require.True(t, ok)

	cases := []struct {
		path, method string
		found        bool
	}{
		{"/v1/users/me/sessions/0195a1b2-0000-7000-8000-000000000001", http.MethodDelete, true},
		{"/v1/users/me/sessions/a/b", http.MethodDelete, false},
		{"/v1/users/me/sessions/", http.MethodDelete, false},
		{"/v1/teams/abc/projects", http.MethodPost, true},
		{"/v1/teams/abc/projects", http.MethodGet, false},
		{"/v1/users/me", http.MethodGet, true},
		{"/v1/users/me", http.MethodPut, false},
		{"/v1/users/me/extra", http.MethodGet, false},
		{"/v1/auth/login", http.MethodPost, false}, // public, so not in the private table
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			assert.Equal(t, c.found, customer.PrivateEndpoint(c.path, c.method) != nil)
		})
	}
}

func TestService_SplitsPublicFromPrivate(t *testing.T) {
	customer, ok := routeTable().Service("customer")
	require.True(t, ok)

	assert.Equal(t, "http://customer:9650", customer.Host)
	assert.NotNil(t, customer.PublicEndpoint("/v1/auth/login", http.MethodPost))
	assert.Nil(t, customer.PrivateEndpoint("/v1/auth/login", http.MethodPost))
	assert.Nil(t, customer.PublicEndpoint("/v1/users/me", http.MethodGet))
	assert.NotNil(t, customer.PrivateEndpoint("/v1/users/me", http.MethodGet))
}

func TestGatewayConfig_Service_UnknownNameIsNotFound(t *testing.T) {
	_, ok := routeTable().Service("billing")

	assert.False(t, ok)
}

func TestGatewayConfig_Service_NameIsCaseSensitive(t *testing.T) {
	_, ok := routeTable().Service("Customer")

	assert.False(t, ok)
}

func TestService_RoutesBelongToOneServiceOnly(t *testing.T) {
	g := routeTable()
	customer, _ := g.Service("customer")
	droplet, _ := g.Service("droplet")

	assert.Equal(t, "http://droplet:9660", droplet.Host)
	assert.NotNil(t, droplet.PrivateEndpoint("/v1/catalog/regions", http.MethodGet))
	assert.NotNil(t, droplet.PrivateEndpoint("/v1/projects/p1/droplets", http.MethodPost))
	assert.Nil(t, customer.PrivateEndpoint("/v1/catalog/regions", http.MethodGet), "a droplet path is unlisted for customer")
	assert.Nil(t, droplet.PrivateEndpoint("/v1/users/me", http.MethodGet), "a customer path is unlisted for droplet")
	assert.Nil(t, droplet.PublicEndpoint("/v1/auth/login", http.MethodPost), "a customer public route is not public on droplet")
}

func TestCompileEndpointPathRegex_WildcardMatchesEverything(t *testing.T) {
	re, err := CompileEndpointPathRegex("*")
	require.NoError(t, err)
	assert.True(t, re.MatchString("/anything/at/all"))
}
