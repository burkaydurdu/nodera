package sendingdomain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
)

const zone = "mta.nodera.io"

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"Example.COM", "example.com", true},
		{" mail.example.com. ", "mail.example.com", true},
		{"bücher.de", "xn--bcher-kva.de", true},
		{"a.b.c.example.co.uk", "a.b.c.example.co.uk", true},
		{"localhost", "", false},
		{"example", "", false},
		{"192.168.1.10", "", false},
		{"[::1]", "", false},
		{"-bad.example.com", "", false},
		{"bad-.example.com", "", false},
		{"under_score.example.com", "", false},
		{"exa mple.com", "", false},
		{"", "", false},
		{"mta.nodera.io", "", false},
		{"x.dkim.mta.nodera.io", "", false},
		{string(make([]byte, 254)), "", false},
	}

	for _, c := range cases {
		got, err := sendingdomain.NormalizeName(c.raw, zone)
		if !c.ok {
			assert.Errorf(t, err, "%q should be refused", c.raw)

			continue
		}
		if assert.NoErrorf(t, err, "%q", c.raw) {
			assert.Equal(t, c.want, got)
		}
	}
}

func TestNewToken_IsEightLowerAlphanumerics(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		token, err := sendingdomain.NewToken()
		assert.NoError(t, err)
		assert.Regexp(t, `^[a-z0-9]{8}$`, token)
		seen[token] = true
	}
	assert.Greater(t, len(seen), 190, "tokens repeat too often")
}
