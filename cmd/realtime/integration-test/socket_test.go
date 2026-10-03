package integrationtest

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

const (
	closeUnauthorized websocket.StatusCode = 4401
	closeAuthTimeout  websocket.StatusCode = 4408
)

// ── Authentication ───────────────────────────────────────────────────────────

func (s *realtimeSuite) TestAuth_TheFirstFrameCarriesTheToken() {
	u := s.signIn(nil)

	s.connect(s.serverA, u.Token)
}

func (s *realtimeSuite) TestAuth_ABadTokenIsClosedAsUnauthorized() {
	sock, _, err := s.dial(s.serverA, webOrigin)
	s.Require().NoError(err)

	sock.send(s, map[string]any{"type": "auth", "token": "not-a-token"})

	s.Equal(closeUnauthorized, sock.closedWith(s))
}

func (s *realtimeSuite) TestAuth_NoTokenInTimeIsClosed() {
	sock, _, err := s.dial(s.serverA, webOrigin)
	s.Require().NoError(err)

	s.Equal(closeAuthTimeout, sock.closedWith(s))
}

func (s *realtimeSuite) TestAuth_ASignedOutSessionIsRefused() {
	u := s.signIn(nil)
	s.Require().NoError(s.rawRedis.Del(context.Background(), "session:"+u.SessionID.String()).Err())
	sock, _, err := s.dial(s.serverA, webOrigin)
	s.Require().NoError(err)

	sock.send(s, map[string]any{"type": "auth", "token": u.Token})

	s.Equal(closeUnauthorized, sock.closedWith(s))
}

func (s *realtimeSuite) TestAuth_AStaleTokenIsRefused() {
	u := s.signIn(nil)
	s.Require().NoError(s.rawRedis.Set(context.Background(), "user_perm_version:"+u.ID.String(), "1", 0).Err())
	sock, _, err := s.dial(s.serverA, webOrigin)
	s.Require().NoError(err)

	sock.send(s, map[string]any{"type": "auth", "token": u.Token})

	s.Equal(closeUnauthorized, sock.closedWith(s), "a Role changed since the token was issued")
}

func (s *realtimeSuite) TestOrigin_AnotherSiteCannotOpenASocket() {
	_, res, err := s.dial(s.serverA, "https://evil.example")

	s.Require().Error(err)
	s.Require().NotNil(res)
	s.Equal(http.StatusForbidden, res.StatusCode)
}

// ── Delivery ─────────────────────────────────────────────────────────────────

func (s *realtimeSuite) TestDelivery_ToAUserReachesEveryTabAndNobodyElse() {
	ayse, can := s.signIn(nil), s.signIn(nil)
	tab1, tab2 := s.connect(s.serverA, ayse.Token), s.connect(s.serverB, ayse.Token)
	other := s.connect(s.serverA, can.Token)

	s.publish(s.a, realtime.ToUser(ayse.ID), "notification.created", map[string]any{"id": "n-1"})

	for _, tab := range []*socket{tab1, tab2} {
		frame := tab.next(s)
		s.Equal("notification.created", frame["type"])
		s.Equal(map[string]any{"id": "n-1"}, frame["data"])
		s.NotEmpty(frame["at"])
		s.Nil(frame["address"], "the address stays on the server")
	}
	s.nothingBut(other, can, s.a)
}

func (s *realtimeSuite) TestDelivery_ToATeamReachesOnlyTheRolesWithThePermission() {
	teamID := uuid.New()
	owner := s.signIn(map[uuid.UUID]authz.Role{teamID: authz.RoleOwner})
	finance := s.signIn(map[uuid.UUID]authz.Role{teamID: authz.RoleFinance})
	developer := s.signIn(map[uuid.UUID]authz.Role{teamID: authz.RoleDeveloper})
	stranger := s.signIn(map[uuid.UUID]authz.Role{uuid.New(): authz.RoleOwner})
	sockets := map[*socket]user{}
	for _, u := range []user{owner, finance, developer, stranger} {
		sockets[s.connect(s.serverB, u.Token)] = u
	}

	s.publish(s.a, realtime.ToTeam(teamID, authz.BillingRead), "billing.status_changed", map[string]any{"status": "suspended"})

	for sock, u := range sockets {
		if u == owner || u == finance {
			s.Equal("billing.status_changed", sock.next(s)["type"])

			continue
		}
		s.nothingBut(sock, u, s.a)
	}
}

func (s *realtimeSuite) TestConsumer_AMessageWithoutAnAddressIsRefused() {
	s.Error(s.publishErr(s.a, realtime.Address{}, "notification.created", map[string]any{}))
	s.Error(s.publishErr(s.a, realtime.Address{TeamID: new(uuid.New())}, "droplet.status_changed", map[string]any{}), "a Team address needs a Permission")
	s.Error(s.publishErr(s.a, realtime.ToUser(uuid.New()), "", map[string]any{}))
}

// ── A connection that outlives its token ─────────────────────────────────────

func (s *realtimeSuite) TestHeartbeat_SigningOutClosesTheSocket() {
	u := s.signIn(nil)
	sock := s.connect(s.serverA, u.Token)

	s.Require().NoError(s.rawRedis.Del(context.Background(), "session:"+u.SessionID.String()).Err())

	s.Equal(closeUnauthorized, sock.closedWith(s))
}

func (s *realtimeSuite) TestHeartbeat_AChangedRoleClosesTheSocket() {
	u := s.signIn(nil)
	sock := s.connect(s.serverA, u.Token)

	s.Require().NoError(s.rawRedis.Set(context.Background(), "user_perm_version:"+u.ID.String(), "1", 0).Err())

	s.Equal(closeUnauthorized, sock.closedWith(s), "the web app refreshes and reconnects with its new Teams")
}

func (s *realtimeSuite) TestReauth_ARefreshedTokenCarriesTheNewTeams() {
	teamID := uuid.New()
	u := s.signIn(nil)
	sock := s.connect(s.serverA, u.Token)

	sock.send(s, map[string]any{"type": "auth", "token": s.token(u, map[uuid.UUID]authz.Role{teamID: authz.RoleDeveloper}, 0)})
	s.Equal("ready", sock.next(s)["type"])
	time.Sleep(300 * time.Millisecond) // past a heartbeat: the socket stays open on the new token

	s.publish(s.b, realtime.ToTeam(teamID, authz.DropletRead), "droplet.status_changed", map[string]any{})
	s.Equal("droplet.status_changed", sock.next(s)["type"])
}

func (s *realtimeSuite) TestReauth_AnotherUsersTokenIsRefused() {
	ayse, can := s.signIn(nil), s.signIn(nil)
	sock := s.connect(s.serverA, ayse.Token)

	sock.send(s, map[string]any{"type": "auth", "token": can.Token})

	s.Equal(closeUnauthorized, sock.closedWith(s))
}

// ── Shutdown ─────────────────────────────────────────────────────────────────

func (s *realtimeSuite) TestShutdown_TellsEverySocketToReconnectElsewhere() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaving, server := s.replica(ctx, logger.Noop(), s.redisClient)
	defer server.Close()
	u := s.signIn(nil)
	sock := s.connect(server, u.Token)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), waitFor)
	defer shutdownCancel()
	s.Require().NoError(leaving.Shutdown(shutdownCtx), "every socket was closed before the replica let go")

	s.Equal(websocket.StatusGoingAway, sock.closedWith(s))
}
