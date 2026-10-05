package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
)

func TestSign_IsHMACOverTimestampAndBody(t *testing.T) {
	secret := []byte("whsec_test")
	body := []byte(`[{"type":"delivered"}]`)

	header := webhook.Sign(secret, 1759665600, body)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("1759665600."))
	mac.Write(body)
	assert.Equal(t, "t=1759665600,v1="+hex.EncodeToString(mac.Sum(nil)), header)
}

func TestNextAttempt_FollowsTheScheduleForADay(t *testing.T) {
	first := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	next, ok := webhook.NextAttempt(first, 1, first)
	require.True(t, ok)
	assert.Equal(t, first.Add(time.Minute), next)

	next, ok = webhook.NextAttempt(first, 6, first.Add(time.Hour))
	require.True(t, ok)
	assert.Equal(t, first.Add(2*time.Hour), next, "the sixth failure waits an hour")

	next, ok = webhook.NextAttempt(first, 20, first.Add(20*time.Hour))
	require.True(t, ok)
	assert.Equal(t, first.Add(24*time.Hour), next, "never past the 24 hours")

	_, ok = webhook.NextAttempt(first, 21, first.Add(24*time.Hour))
	assert.False(t, ok, "dropped once 24 hours have passed")
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		raw       string
		allowHTTP bool
		ok        bool
	}{
		{"https://hooks.example.com/nodera", false, true},
		{"http://hooks.example.com/nodera", false, false},
		{"http://hooks.example.com/nodera", true, true},
		{"https://user:pass@hooks.example.com/", false, false},
		{"https://127.0.0.1/", false, false},
		{"https://10.0.0.5/hook", false, false},
		{"https://[::1]/hook", false, false},
		{"ftp://hooks.example.com/", false, false},
		{"not a url", false, false},
	}

	for _, c := range cases {
		err := webhook.ValidateURL(c.raw, c.allowHTTP)
		if c.ok {
			assert.NoErrorf(t, err, c.raw)
		} else {
			assert.Errorf(t, err, c.raw)
		}
	}
}

func TestPublicIP(t *testing.T) {
	for _, ip := range []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "fc00::1", "0.0.0.0",
		"198.18.0.1", "240.0.0.1", "255.255.255.255", "192.0.2.10", "::ffff:127.0.0.1", "64:ff9b::a9fe:a9fe", "2002:7f00:1::", "2001:db8::1",
	} {
		assert.Falsef(t, webhook.PublicIP(net.ParseIP(ip)), ip)
	}
	for _, ip := range []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		assert.Truef(t, webhook.PublicIP(net.ParseIP(ip)), ip)
	}
}
