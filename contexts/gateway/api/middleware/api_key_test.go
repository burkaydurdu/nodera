package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/logger"
	mockpersistence "github.com/piggystats/nodera/mock/persistence"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/pkg/echo_middleware"
)

const (
	testKeyID     = "Ab3dE5gH7jK9"
	testKeySecret = "4fZ9kQ2mXw7RtB1nLp8sVc3yHd6gJe0aUo5iKq2Tz9W"
	// unexpectedError is apperror.ErrorCommon as the caller reads it.
	unexpectedError = `{"code":10000,"domain":100,"message":"unexpected error"}`
)

// The parser is a pure function of the header value; each row is one way a
// value can fail to be a key that was ever issued.
func TestParseAPIKey(t *testing.T) {
	key := "nd_" + testKeyID + "." + testKeySecret

	cases := []struct {
		name   string
		header string
		ok     bool
		secret string
	}{
		{"an issued key", "Bearer " + key, true, testKeySecret},
		{"a secret whose rendering dropped leading zeros", "Bearer nd_" + testKeyID + "." + testKeySecret[:41], true, testKeySecret[:41]},
		{"no header", "", false, ""},
		{"another scheme", "Basic " + key, false, ""},
		{"the scheme in lower case", "bearer " + key, false, ""},
		{"no space after the scheme", "Bearer" + key, false, ""},
		{"two spaces after the scheme", "Bearer  " + key, false, ""},
		{"a trailing space", "Bearer " + key + " ", false, ""},
		{"a trailing newline", "Bearer " + key + "\n", false, ""},
		{"the scheme alone", "Bearer ", false, ""},
		{"no prefix", "Bearer " + testKeyID + "." + testKeySecret, false, ""},
		{"the prefix in upper case", "Bearer ND_" + testKeyID + "." + testKeySecret, false, ""},
		{"an id one short", "Bearer nd_" + testKeyID[:11] + "." + testKeySecret, false, ""},
		{"an id one long", "Bearer nd_" + testKeyID + "X." + testKeySecret, false, ""},
		{"an id outside base62", "Bearer nd_Ab3dE5gH7j-9." + testKeySecret, false, ""},
		{"no separator", "Bearer nd_" + testKeyID + testKeySecret, false, ""},
		{"the public prefix alone", "Bearer nd_" + testKeyID, false, ""},
		{"an empty secret", "Bearer nd_" + testKeyID + ".", false, ""},
		{"a secret one too long", "Bearer " + key + "x", false, ""},
		{"a second separator", "Bearer " + key[:30] + "." + key[31:], false, ""},
		{"a secret outside base62", "Bearer nd_" + testKeyID + "." + strings.Replace(testKeySecret, "Z", "_", 1), false, ""},
		{"a non-ASCII letter in the secret", "Bearer nd_" + testKeyID + "." + strings.Replace(testKeySecret, "Z", "é", 1), false, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, secret, ok := parseAPIKey(c.header)

			assert.Equal(t, c.ok, ok)
			if c.ok {
				assert.Equal(t, testKeyID, id)
				assert.Equal(t, c.secret, secret)
			} else {
				assert.Empty(t, id)
				assert.Empty(t, secret)
			}
		})
	}
}

// chainServer runs the given middlewares in front of a handler that answers
// 202, with the platform's error handler rendering whatever they return.
func chainServer(middlewares ...echo.MiddlewareFunc) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = echo_middleware.ErrorHandler(logger.Noop())
	e.Use(middlewares...)
	e.POST("/v1/mail/messages", func(c echo.Context) error {
		return c.NoContent(http.StatusAccepted)
	})

	return e
}

func sendThrough(e *echo.Echo) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/mail/messages", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer nd_"+testKeyID+"."+testKeySecret)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

func withAuthenticatedKey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Set(authenticatedAPIKey.String(), authenticatedKey{id: testKeyID, permissions: []string{"mail.send"}})

		return next(c)
	}
}

// A Redis outage says nothing about the key; a 401 would tell software to
// give up on a good one.
func TestAPIKey_RedisUnreachable_IsAServerErrorNotA401(t *testing.T) {
	ctrl := gomock.NewController(t)
	cache := mockpersistence.NewMockIService(ctrl)
	cache.EXPECT().GetString(gomock.Any(), "api_key:"+testKeyID).Return("", errors.New("connection refused"))

	rec := sendThrough(chainServer(APIKey(cache)))

	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.JSONEq(t, unexpectedError, rec.Body.String())
}

func TestAPIKey_UnreadableRecord_IsAServerError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cache := mockpersistence.NewMockIService(ctrl)
	cache.EXPECT().GetString(gomock.Any(), "api_key:"+testKeyID).Return("{not json", nil)

	rec := sendThrough(chainServer(APIKey(cache)))

	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.JSONEq(t, unexpectedError, rec.Body.String())
}

func TestAPIKey_NoRecord_IsUnauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	cache := mockpersistence.NewMockIService(ctrl)
	cache.EXPECT().GetString(gomock.Any(), "api_key:"+testKeyID).Return("", nil)

	rec := sendThrough(chainServer(APIKey(cache)))

	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"code":401,"domain":100,"message":"Unauthorized"}`, rec.Body.String())
}

// An upstream that cannot be reached is answered by the platform's error
// handler, like every other error the gateway answers.
func TestIntegrationReverseProxy_UpstreamUnreachable_IsABadGatewayInThePlatformShape(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()
	proxies, err := SetupProxies(&config.GatewayConfig{Gateway: config.Gateway{Services: map[string]config.Service{
		"mail": {Host: upstream.URL},
	}}}, NewIntegrationReverseProxy(logger.Noop()))
	require.NoError(t, err)
	toUpstream := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(constants.TargetHost.String(), upstream.URL)

			return next(c)
		}
	}

	rec := sendThrough(chainServer(toUpstream, Proxy(proxies)))

	assert.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"code":502,"domain":100,"message":"Bad Gateway"}`, rec.Body.String())
}

func TestAPIKeyRateLimit_RedisUnreachable_LetsTheRequestThrough(t *testing.T) {
	ctrl := gomock.NewController(t)
	cache := mockpersistence.NewMockIService(ctrl)
	cache.EXPECT().
		SlidingWindowAllow(gomock.Any(), "ratelimit:integration:"+testKeyID, time.Second, int64(100)).
		Return(false, errors.New("connection refused"))

	rec := sendThrough(chainServer(withAuthenticatedKey, APIKeyRateLimit(cache, logger.Noop())))

	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

func TestRecordAPIKeyUse_WriteFails_TheRequestStillGoesOn(t *testing.T) {
	ctrl := gomock.NewController(t)
	cache := mockpersistence.NewMockIService(ctrl)
	cache.EXPECT().
		Set(gomock.Any(), "api_key_last_used:"+testKeyID, gomock.Any(), time.Duration(0)).
		Return(errors.New("connection refused"))

	rec := sendThrough(chainServer(withAuthenticatedKey, RecordAPIKeyUse(cache, logger.Noop())))

	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}
