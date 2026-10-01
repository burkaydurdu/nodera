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

func TestEndpoint_Param_ReadsTheSegmentUnderTheName(t *testing.T) {
	customer, _ := routeTable().Service("customer")
	endpoint := customer.PrivateEndpoint("/v1/teams/019a0000-0000-7000-8000-00000000c0de/projects", http.MethodPost)
	require.NotNil(t, endpoint)

	teamID, ok := endpoint.Param("/v1/teams/019a0000-0000-7000-8000-00000000c0de/projects", TeamIDParam)

	assert.True(t, ok)
	assert.Equal(t, "019a0000-0000-7000-8000-00000000c0de", teamID)
}

func TestEndpoint_Param_UnknownNameIsNotFound(t *testing.T) {
	customer, _ := routeTable().Service("customer")
	endpoint := customer.PrivateEndpoint("/v1/teams/abc/projects", http.MethodPost)
	require.NotNil(t, endpoint)

	_, ok := endpoint.Param("/v1/teams/abc/projects", "projectID")

	assert.False(t, ok)
}

func TestEndpoint_Param_PathThatDoesNotMatchIsNotFound(t *testing.T) {
	customer, _ := routeTable().Service("customer")
	endpoint := customer.PrivateEndpoint("/v1/teams/abc/projects", http.MethodPost)
	require.NotNil(t, endpoint)

	_, ok := endpoint.Param("/v1/users/me", TeamIDParam)

	assert.False(t, ok)
}

func TestEndpoint_Param_SeveralParamsAreToldApart(t *testing.T) {
	g := &GatewayConfig{Gateway: Gateway{Services: map[string]Service{
		"customer": {Host: "http://customer:9650", Endpoints: Endpoints{
			{Path: "/v1/teams/:teamID/members/:userID", Method: "DELETE"},
		}},
	}}}
	require.NoError(t, g.Validate())
	customer, _ := g.Service("customer")
	endpoint := customer.PrivateEndpoint("/v1/teams/t1/members/u1", http.MethodDelete)
	require.NotNil(t, endpoint)

	teamID, _ := endpoint.Param("/v1/teams/t1/members/u1", TeamIDParam)
	userID, _ := endpoint.Param("/v1/teams/t1/members/u1", "userID")

	assert.Equal(t, "t1", teamID)
	assert.Equal(t, "u1", userID)
}

func TestValidate_PermissionsOnATeamRoute_Pass(t *testing.T) {
	g := &GatewayConfig{Gateway: Gateway{Services: map[string]Service{
		"customer": {Host: "http://customer:9650", Endpoints: Endpoints{
			{Path: "/v1/teams/:teamID/invitations", Method: "POST", Permissions: []string{"team.manage"}},
			{Path: "/v1/teams/:teamID/projects", Method: "GET", Permissions: []string{"team.read"}},
		}},
	}}}

	assert.NoError(t, g.Validate())
}

func TestValidate_PermissionsWithoutTeamIDInPath_IsRefused(t *testing.T) {
	g := &GatewayConfig{Gateway: Gateway{Services: map[string]Service{
		"droplet": {Host: "http://droplet:9660", Endpoints: Endpoints{
			{Path: "/v1/droplets/:dropletID", Method: "DELETE", Permissions: []string{"droplet.write"}},
		}},
	}}}

	err := g.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "DELETE /v1/droplets/:dropletID")
	assert.Contains(t, err.Error(), ":teamID")
}

func TestValidate_UnknownPermission_IsRefused(t *testing.T) {
	g := &GatewayConfig{Gateway: Gateway{Services: map[string]Service{
		"customer": {Host: "http://customer:9650", Endpoints: Endpoints{
			{Path: "/v1/teams/:teamID/projects", Method: "POST", Permissions: []string{"project.create"}},
		}},
	}}}

	err := g.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"project.create"`)
}

func TestValidate_MissingPathOrMethod_IsRefused(t *testing.T) {
	g := &GatewayConfig{Gateway: Gateway{Services: map[string]Service{
		"customer": {Host: "http://customer:9650", Endpoints: Endpoints{{Path: "/v1/users/me"}}},
	}}}

	assert.Error(t, g.Validate())
}
