package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
)

func (s *kubernetesSuite) TestBilling_OpeningNeedsAPaymentMethod() {
	p := s.grantProjectWithoutBilling()

	s.expectError(s.open(p, "prod"), http.StatusPaymentRequired, 71, 10003)
	s.Empty(s.engine.Calls())

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.expectError(s.open(p, "prod"), http.StatusLocked, 71, 10004)
}

// A suspended Team's Clusters refuse anything that would be charged anew; the
// droplet context powers their Nodes off meanwhile. Paying brings changes back.
func (s *kubernetesSuite) TestBilling_SuspensionRefusesChangesAndPaymentLiftsIt() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	s.joined(p, id, nodes)
	pool := s.poolID(p, id, 0)
	s.clearOutbox()

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.Equal("suspended", s.get(p, id).str("billing_status"))
	s.Equal("degraded", s.get(p, id).str("status"), "the droplet context is powering the Nodes off")
	s.expectError(s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"batch","plan":"c8-64","count":1}`, p.Owner), http.StatusLocked, 71, 10004)
	s.expectError(s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+pool+"/count"), `{"count":3}`, p.Owner), http.StatusLocked, 71, 10004)
	s.Equal(http.StatusOK, s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+pool+"/count"), `{"count":1}`, p.Owner).Code, "releasing a Node is never refused")
	s.Equal(http.StatusOK, s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", p.Owner).Code, "reaching the cluster is not charged")
	s.Contains(s.realtimeTypes(), constants.RealtimeClusterChanged)

	s.setBilling(p.TeamID, "active", time.Now().Add(time.Second))
	s.Equal(http.StatusOK, s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+pool+"/count"), `{"count":3}`, p.Owner).Code)
	s.Equal("running", s.get(p, id).str("status"), "the kubelet that stayed ready counts again")
}

func (s *kubernetesSuite) TestBilling_AnOlderStatusChangesNothing() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")

	s.setBilling(p.TeamID, "suspended", time.Now().Add(-time.Hour))

	s.Equal("active", s.get(p, id).str("billing_status"))
}

func (s *kubernetesSuite) TestBilling_NoPaymentMethodKeepsTheClusterRunning() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	pool := s.poolID(p, id, 0)

	s.setBilling(p.TeamID, "no_payment_method", time.Now())

	s.Equal(http.StatusOK, s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", p.Owner).Code)
	s.expectError(s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+pool+"/count"), `{"count":3}`, p.Owner), http.StatusPaymentRequired, 71, 10003)
	s.expectError(s.open(p, "another"), http.StatusPaymentRequired, 71, 10003)
}

// Day 21 of Dunning deletes the Team's Clusters with its Droplets.
func (s *kubernetesSuite) TestBilling_DeletionDayDeletesTheClusters() {
	p := s.grantProject()
	a, nodes := s.ready(p, "prod")
	s.joined(p, a, nodes)
	b := s.opened(p, "staging").str("id")
	s.setBilling(p.TeamID, "suspended", time.Now())

	s.consume(constants.TeamDropletsDeletionRequested, fmt.Sprintf(`{"payload":{"team_id":%q,"requested_at":%q}}`, p.TeamID, time.Now().UTC().Format(time.RFC3339Nano)))

	s.Equal("deleting", s.get(p, a).str("status"))
	s.Equal("deleting", s.get(p, b).str("status"))
	s.release()
	s.Len(s.outboxPayloads(constants.NodeReleased), 2, "the running Cluster's Nodes are given back; the other never had Droplets")
}
