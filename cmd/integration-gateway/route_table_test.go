package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/cmd/integration-gateway/config"
)

// A route is not shipped until every environment's table carries it, under
// the right service and behind the sending API's port (9701, not the
// management API's 9700).
func TestRouteTables_EveryEnvironmentSendsMailThroughTheSendingAPI(t *testing.T) {
	hosts := map[string]string{
		"dev":   "http://localhost:9701",
		"test":  "http://localhost:9701",
		"alpha": "http://nodera-mail:9701",
		"prod":  "http://nodera-mail:9701",
	}

	for env, host := range hosts {
		t.Run(env, func(t *testing.T) {
			table := config.InitGatewayConfig(env).ConvertToGateway()
			require.NoError(t, table.Validate())

			route, ok := table.Route("/v1/mail/messages", http.MethodPost)

			require.True(t, ok)
			assert.Equal(t, "mail", route.Service)
			assert.Equal(t, host, route.Host)
			assert.Equal(t, []string{"mail.send"}, route.Endpoint.Permissions)
		})
	}
}
