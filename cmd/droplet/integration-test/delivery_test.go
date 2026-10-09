package integrationtest

import (
	"context"

	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/providers"
)

// A server reads powered off for a moment at the end of its install. The
// Droplet stays provisioning and is asked again, rather than settling as off
// and never being asked again while the server runs and is never charged.
func (s *dropletSuite) TestDelivery_AServerReadOffBeforeItsFirstBootIsStillBeingDelivered() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	created := s.createDroplet(u, projectID, "web-1")
	id := created.str("id")
	ref := s.providerRef(id)

	s.fake.SetStatus(ref, providers.StatusOff)
	s.poll()
	s.Equal("provisioning", s.get("/v1/droplets/"+id, u.Token).str("status"))
	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStarted), "nothing ran, nothing is charged")

	s.fake.SetStatus(ref, providers.StatusActive)
	s.poll()
	s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"))
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStarted))
}

// A Droplet a poll left off before it ever ran, before such a reading counted
// as still provisioning, is asked again and comes back when its server is on.
func (s *dropletSuite) TestDelivery_AnOffDropletThatNeverRanIsAskedAgain() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	created := s.createDroplet(u, projectID, "web-1")
	id := created.str("id")
	ref := s.providerRef(id)
	_, err := s.pgPool.Exec(context.Background(), `UPDATE droplets SET status = 'off' WHERE id = $1`, id)
	s.Require().NoError(err)

	s.fake.SetStatus(ref, providers.StatusActive)
	s.poll()

	detail := s.get("/v1/droplets/"+id, u.Token)
	s.Equal("active", detail.str("status"))
	s.NotEmpty(detail.str("active_at"))
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStarted), "the hours it runs are charged from now")
}
