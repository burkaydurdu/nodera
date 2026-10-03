package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// newEchoSocketUpstream is a realtime service that echoes every text frame.
func newEchoSocketUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			kind, raw, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), kind, raw); err != nil {
				return
			}
		}
	}))
}

// A browser cannot set headers on a WebSocket handshake, so the socket's route
// is headerless and public; the realtime service authenticates the first frame.
func (s *gatewaySuite) TestRealtime_TheSocketPassesThroughWithoutHeaders() {
	gateway := httptest.NewServer(s.server)
	defer gateway.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/realtime", nil)
	s.Require().NoError(err)
	defer func() { _ = conn.CloseNow() }()

	s.Require().NoError(conn.Write(ctx, websocket.MessageText, []byte(`{"type":"auth"}`)))
	_, echoed, err := conn.Read(ctx)
	s.Require().NoError(err)
	s.Equal(`{"type":"auth"}`, string(echoed), "frames flow both ways once the gateway hands the connection over")
}

func (s *gatewaySuite) TestNotification_TheInboxIsTheNotificationServices() {
	rec := s.do(http.MethodGet, "/v1/notifications", s.token(liveSessionID, "access", time.Minute), targetNotification)

	s.Equal(http.StatusOK, rec.Code, rec.Body.String())
	s.Equal("notification", rec.Header().Get("X-Upstream"))
}
