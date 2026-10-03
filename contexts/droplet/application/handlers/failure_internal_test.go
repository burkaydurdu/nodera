package handlers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
)

func TestCustomerSafeFailure(t *testing.T) {
	cases := []struct {
		name   string
		cause  error
		code   droplet.FailureCode
		reason string
	}{
		{"a refusal the Provider explained", provider.ErrorRejected.Copy().SetError(errors.New("insufficient balance")), droplet.FailureRejected, provider.ErrorRejected.Message},
		{"the Provider unreachable", provider.ErrorUnavailable, droplet.FailureUnavailable, provider.ErrorUnavailable.Message},
		{"another Provider error", provider.ErrorInvalidRef, droplet.FailureUnavailable, provider.ErrorInvalidRef.Message},
		{"anything else", errors.New("dial tcp: connection refused"), droplet.FailureUnavailable, provider.ErrorUnavailable.Message},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, reason := customerSafeFailure(c.cause)

			assert.Equal(t, c.code, code)
			assert.Equal(t, c.reason, reason)
		})
	}
}
