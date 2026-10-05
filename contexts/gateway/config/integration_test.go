package config

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func integrationTable(services map[string]Service) *IntegrationGatewayConfig {
	return &IntegrationGatewayConfig{Gateway: Gateway{Name: "nodera-integration-gateway", Services: services}}
}

// mail.send is the only Permission an API key can carry today, so the
// droplet route borrows it.
func validIntegrationTable(t *testing.T) *IntegrationGatewayConfig {
	t.Helper()
	g := integrationTable(map[string]Service{
		"mail": {Host: "http://mail:9701", Endpoints: Endpoints{
			{Path: "/v1/mail/messages", Method: "POST", Permissions: []string{"mail.send"}},
			{Path: "/v1/mail/messages/:messageID", Method: "GET", Permissions: []string{"mail.send"}},
		}},
		"droplet": {Host: "https://droplet:9661", Endpoints: Endpoints{
			{Path: "/v1/droplets/:dropletID", Method: "GET", Permissions: []string{"mail.send"}},
		}},
	})
	require.NoError(t, g.Validate())

	return g
}

func TestIntegrationGatewayConfig_Validate_AKeyGuardedTablePasses(t *testing.T) {
	g := integrationTable(map[string]Service{
		"mail": {Host: "http://localhost:9701", Endpoints: Endpoints{
			{Path: "/v1/mail/messages", Method: "POST", Permissions: []string{"mail.send"}},
		}},
	})

	assert.NoError(t, g.Validate())
}

func TestIntegrationGatewayConfig_Route_ResolvesTheServiceByMethodAndPathAlone(t *testing.T) {
	g := validIntegrationTable(t)

	route, ok := g.Route("/v1/mail/messages", http.MethodPost)
	require.True(t, ok)
	assert.Equal(t, "mail", route.Service)
	assert.Equal(t, "http://mail:9701", route.Host)
	assert.Equal(t, []string{"mail.send"}, route.Endpoint.Permissions)

	route, ok = g.Route("/v1/droplets/019a0000-0000-7000-8000-00000000d0d0", http.MethodGet)
	require.True(t, ok)
	assert.Equal(t, "droplet", route.Service)
	assert.Equal(t, "https://droplet:9661", route.Host)
}

func TestIntegrationGatewayConfig_Route_UnlistedRequestsAreNotFound(t *testing.T) {
	g := validIntegrationTable(t)

	cases := []struct{ path, method string }{
		{"/v1/mail/messages", http.MethodGet},
		{"/v1/mail/messages/", http.MethodPost},
		{"/v1/mail/messages/m1/extra", http.MethodGet},
		{"/v1/mail/messages/", http.MethodGet},
		{"/v1/mail", http.MethodPost},
		{"/V1/mail/messages", http.MethodPost},
		{"/v1/droplets/d1", http.MethodDelete},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			_, ok := g.Route(c.path, c.method)

			assert.False(t, ok)
		})
	}
}

func TestIntegrationGatewayConfig_Validate_RouteWithoutPermissions_IsRefused(t *testing.T) {
	g := integrationTable(map[string]Service{
		"mail": {Host: "http://localhost:9701", Endpoints: Endpoints{
			{Path: "/v1/mail/messages", Method: "POST"},
		}},
	})

	err := g.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "POST /v1/mail/messages")
	assert.Contains(t, err.Error(), "permissions must be listed")
}

func TestIntegrationGatewayConfig_Validate_UnknownPermission_IsRefused(t *testing.T) {
	g := integrationTable(map[string]Service{
		"mail": {Host: "http://localhost:9701", Endpoints: Endpoints{
			{Path: "/v1/mail/messages", Method: "POST", Permissions: []string{"mail.sned"}},
		}},
	})

	err := g.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown permission "mail.sned"`)
}

// A Role's Permission that no API key may carry would lock every key out.
func TestIntegrationGatewayConfig_Validate_PermissionAKeyCannotCarry_IsRefused(t *testing.T) {
	g := integrationTable(map[string]Service{
		"mail": {Host: "http://localhost:9701", Endpoints: Endpoints{
			{Path: "/v1/mail/messages", Method: "POST", Permissions: []string{"mail.send", "mail.write"}},
		}},
	})

	err := g.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), `permission "mail.write" is not one an API key can carry`)
}

// Nothing behind this gateway is reachable without a key, and there are no
// target headers to do without.
func TestIntegrationGatewayConfig_Validate_PublicOrHeaderlessRoute_IsRefused(t *testing.T) {
	cases := map[string]Endpoint{
		"public":     {Path: "/v1/mail/messages", Method: "POST", Public: true, Permissions: []string{"mail.send"}},
		"headerless": {Path: "/v1/mail/messages", Method: "POST", Headerless: true, Permissions: []string{"mail.send"}},
	}

	for name, endpoint := range cases {
		t.Run(name, func(t *testing.T) {
			g := integrationTable(map[string]Service{
				"mail": {Host: "http://localhost:9701", Endpoints: Endpoints{endpoint}},
			})

			err := g.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "none may be public or headerless")
		})
	}
}

func TestIntegrationGatewayConfig_Validate_MissingPathOrMethod_IsRefused(t *testing.T) {
	for name, endpoint := range map[string]Endpoint{
		"no path":   {Method: "POST", Permissions: []string{"mail.send"}},
		"no method": {Path: "/v1/mail/messages", Permissions: []string{"mail.send"}},
	} {
		t.Run(name, func(t *testing.T) {
			g := integrationTable(map[string]Service{
				"mail": {Host: "http://localhost:9701", Endpoints: Endpoints{endpoint}},
			})

			assert.Error(t, g.Validate())
		})
	}
}

func TestIntegrationGatewayConfig_Validate_UpstreamThatIsNotAnHTTPURL_IsRefused(t *testing.T) {
	for _, host := range []string{"", "localhost:9701", "nodera-mail", "ftp://nodera-mail:9701", "http://"} {
		t.Run(host, func(t *testing.T) {
			g := integrationTable(map[string]Service{
				"mail": {Host: host, Endpoints: Endpoints{
					{Path: "/v1/mail/messages", Method: "POST", Permissions: []string{"mail.send"}},
				}},
			})

			err := g.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), `service "mail"`)
		})
	}
}

// Whether routes of two services could both claim one request is a pure
// function of their methods and path patterns.
func TestIntegrationGatewayConfig_Validate_RoutesOfTwoServices_RefusedWhenTheyCanMatchTheSameRequest(t *testing.T) {
	cases := []struct {
		name           string
		methodA, pathA string
		methodB, pathB string
		refused        bool
	}{
		{"same literal path", "POST", "/v1/messages", "POST", "/v1/messages", true},
		{"method case differs", "POST", "/v1/messages", "post", "/v1/messages", true},
		{"param against literal", "GET", "/v1/messages/:messageID", "GET", "/v1/messages/batch", true},
		{"param against param", "GET", "/v1/:product/status", "GET", "/v1/:area/status", true},
		{"wildcard against anything", "POST", "*", "POST", "/v1/mail/messages", true},
		{"different literals", "POST", "/v1/mail/messages", "POST", "/v1/droplets", false},
		{"different methods", "POST", "/v1/messages", "GET", "/v1/messages", false},
		{"different segment counts", "GET", "/v1/messages/:messageID", "GET", "/v1/messages/:messageID/events", false},
		{"param cannot reach a different literal", "GET", "/v1/mail/:x", "GET", "/v1/droplets/list", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := integrationTable(map[string]Service{
				"mail":    {Host: "http://mail:9701", Endpoints: Endpoints{{Path: c.pathA, Method: c.methodA, Permissions: []string{"mail.send"}}}},
				"droplet": {Host: "http://droplet:9661", Endpoints: Endpoints{{Path: c.pathB, Method: c.methodB, Permissions: []string{"mail.send"}}}},
			})

			err := g.Validate()

			if c.refused {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "(mail)")
				assert.Contains(t, err.Error(), "(droplet)")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Within one service the upstream is the same whichever route matches.
func TestIntegrationGatewayConfig_Validate_OverlappingRoutesInOneService_Pass(t *testing.T) {
	g := integrationTable(map[string]Service{
		"mail": {Host: "http://mail:9701", Endpoints: Endpoints{
			{Path: "/v1/mail/messages/:messageID", Method: "GET", Permissions: []string{"mail.send"}},
			{Path: "/v1/mail/messages/batch", Method: "GET", Permissions: []string{"mail.send"}},
		}},
	})

	assert.NoError(t, g.Validate())
}
