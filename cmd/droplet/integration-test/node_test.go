package integrationtest

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
)

// A Node Droplet is what the droplet context opens at the kubernetes
// context's request (docs/adr/0014): a Droplet of the Project, ordered with
// the cloud-init left in Redis and Nodera's operational key only.

type nodeOrder struct {
	NodeID    uuid.UUID
	ClusterID uuid.UUID
	ProjectID uuid.UUID
	TeamID    uuid.UUID
}

func (s *dropletSuite) newNodeOrder(teamID, projectID uuid.UUID) nodeOrder {
	return nodeOrder{NodeID: uuid.New(), ClusterID: uuid.New(), ProjectID: projectID, TeamID: teamID}
}

func (s *dropletSuite) leaveUserData(nodeID uuid.UUID, userData string) {
	s.Require().NoError(s.rawRedis.Set(context.Background(), fmt.Sprintf(constants.UserDataKeyFormat, nodeID), userData, time.Hour).Err())
}

func (s *dropletSuite) requestNode(o nodeOrder, hostname string) error {
	return s.consumeErr(constants.NodeRequested, fmt.Sprintf(
		`{"payload":{"node_id":%q,"cluster_id":%q,"pool":"workers","project_id":%q,"team_id":%q,"plan":"c4-32","region":"vno1","image":"ubuntu-24-04","hostname":%q,"requested_at":%q}}`,
		o.NodeID, o.ClusterID, o.ProjectID, o.TeamID, hostname, time.Now().UTC().Format(time.RFC3339Nano)))
}

func (s *dropletSuite) releaseNode(nodeID uuid.UUID) error {
	return s.consumeErr(constants.NodeReleased, fmt.Sprintf(`{"payload":{"node_id":%q,"released_at":%q}}`, nodeID, time.Now().UTC().Format(time.RFC3339Nano)))
}

// nodeDroplet answers the one Droplet listed for the Project.
func (s *dropletSuite) nodeDroplet(u user, projectID uuid.UUID) map[string]any {
	list := s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List
	s.Require().Len(list, 1)

	return list[0]
}

func (s *dropletSuite) TestNode_ARequestOpensANodeDropletWithItsCloudInitAndTheOperationalKey() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	o := s.newNodeOrder(teamID, projectID)
	s.leaveUserData(o.NodeID, "#cloud-config\nruncmd: [kubeadm join]")

	s.Require().NoError(s.requestNode(o, "workers-a1b2"))

	s.Require().Len(s.fake.Specs, 1, "one server was ordered")
	s.Equal("#cloud-config\nruncmd: [kubeadm join]", s.fake.Specs[0].UserData)
	s.Equal("workers-a1b2", s.fake.Specs[0].Hostname)
	s.Len(s.fake.Specs[0].SSHKeyRefs, 1, "the operational key and nothing else")

	d := s.nodeDroplet(u, projectID)
	s.Equal("provisioning", d["status"])
	s.Equal("c4-32", d["plan_code"])
	s.Empty(d["ssh_keys"], "no customer key is on a Node Droplet")
	node, _ := d["node"].(map[string]any)
	s.Equal(o.ClusterID.String(), node["cluster_id"])
	s.Equal(o.NodeID.String(), node["node_id"])
	s.Equal("workers", node["pool"])

	s.fake.Advance()
	s.poll()
	s.Equal("active", s.nodeDroplet(u, projectID)["status"])

	changes := s.statusChanges(fmt.Sprint(d["id"]))
	s.Require().Len(changes, 2)
	for _, change := range changes {
		s.Equal(o.NodeID.String(), change["node_id"], "the kubernetes context follows the Node by its own id")
		s.Equal(o.ClusterID.String(), change["cluster_id"])
	}
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStarted), "a Node is charged as the Droplet it is")
}

func (s *dropletSuite) TestNode_ARedeliveredRequestOpensNothingTwice() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	o := s.newNodeOrder(teamID, projectID)
	s.leaveUserData(o.NodeID, "#cloud-config")

	s.Require().NoError(s.requestNode(o, "workers-a1b2"))
	s.Require().NoError(s.requestNode(o, "workers-a1b2"))

	s.Len(s.fake.Specs, 1)
	s.Len(s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List, 1)
}

func (s *dropletSuite) TestNode_ARequestForATeamWithoutAPaymentMethodIsRejected() {
	u := s.signIn()
	teamID, projectID := s.grantProjectWithoutBilling(u)
	o := s.newNodeOrder(teamID, projectID)
	s.leaveUserData(o.NodeID, "#cloud-config")

	s.Require().NoError(s.requestNode(o, "workers-a1b2"), "a rejection is an answer, not a failure to consume")

	s.Empty(s.fake.Specs, "nothing was ordered")
	s.Empty(s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List, "nothing was recorded")
	rejected := s.outboxPayloads(constants.NodeRejected)
	s.Require().Len(rejected, 1)
	s.Equal(o.NodeID.String(), rejected[0]["node_id"])
	s.Equal(o.ClusterID.String(), rejected[0]["cluster_id"])
	s.Equal(droplet.ErrorPaymentMethodRequired.Message, rejected[0]["reason"])
	s.isRFC3339UTC(rejected[0]["rejected_at"])
}

func (s *dropletSuite) TestNode_ARequestWhoseJoinSecretExpiredIsRejected() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	o := s.newNodeOrder(teamID, projectID)

	s.Require().NoError(s.requestNode(o, "workers-a1b2"))

	s.Empty(s.fake.Specs)
	rejected := s.outboxPayloads(constants.NodeRejected)
	s.Require().Len(rejected, 1)
	s.Contains(rejected[0]["reason"], "join secret expired")
}

func (s *dropletSuite) TestNode_ARequestForAPlanTheCatalogDoesNotSellIsRejected() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	o := s.newNodeOrder(teamID, projectID)
	s.leaveUserData(o.NodeID, "#cloud-config")

	s.Require().NoError(s.consumeErr(constants.NodeRequested, fmt.Sprintf(
		`{"payload":{"node_id":%q,"cluster_id":%q,"pool":"workers","project_id":%q,"team_id":%q,"plan":"no-such-plan","region":"vno1","image":"ubuntu-24-04","hostname":"workers-1","requested_at":%q}}`,
		o.NodeID, o.ClusterID, o.ProjectID, o.TeamID, time.Now().UTC().Format(time.RFC3339Nano))))

	rejected := s.outboxPayloads(constants.NodeRejected)
	s.Require().Len(rejected, 1)
	s.Equal("plan not found", rejected[0]["reason"])
}

func (s *dropletSuite) TestNode_AMalformedRequestIsRefused() {
	s.Error(s.consumeErr(constants.NodeRequested, `{"payload":{"node_id":"not-a-uuid"}}`))
	s.Error(s.consumeErr(constants.NodeRequested, fmt.Sprintf(`{"payload":{"node_id":%q}}`, uuid.New())), "the catalog codes are missing")
}

func (s *dropletSuite) TestNode_ACustomerCannotPowerOrDeleteANodeDroplet() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	o := s.newNodeOrder(teamID, projectID)
	s.leaveUserData(o.NodeID, "#cloud-config")
	s.Require().NoError(s.requestNode(o, "workers-a1b2"))
	s.fake.Advance()
	s.poll()
	id := fmt.Sprint(s.nodeDroplet(u, projectID)["id"])

	for _, res := range []*response{
		s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"power_off"}`, u.Token),
		s.request(http.MethodDelete, "/v1/droplets/"+id, "", u.Token),
	} {
		s.Equal(droplet.ErrorManagedByCluster.HttpStatus, res.Code, res.Raw)
		domain, code := res.errorCode()
		s.Equal(int(droplet.Domain), domain)
		s.Equal(droplet.ErrorManagedByCluster.Code, code)
	}
	s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"))
	s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"))
}

func (s *dropletSuite) TestNode_AReleaseGivesTheDropletBack() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	o := s.newNodeOrder(teamID, projectID)
	s.leaveUserData(o.NodeID, "#cloud-config")
	s.Require().NoError(s.requestNode(o, "workers-a1b2"))
	s.fake.Advance()
	s.poll()
	id := fmt.Sprint(s.nodeDroplet(u, projectID)["id"])

	s.Require().NoError(s.releaseNode(o.NodeID))
	s.Equal("deleting", s.get("/v1/droplets/"+id, u.Token).str("status"))
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStopped), "the meter stops at the release")

	s.Require().NoError(s.releaseNode(o.NodeID), "a redelivered release changes nothing")
	s.Require().NoError(s.releaseNode(uuid.New()), "a Node this context never opened needs nothing")

	s.poll()
	s.Equal("deleted", s.get("/v1/droplets/"+id, u.Token).str("status"))
	deleted := s.statusChanges(id)
	s.Equal("deleted", deleted[len(deleted)-1]["status"])
	s.Equal(o.NodeID.String(), deleted[len(deleted)-1]["node_id"])
}

func (s *dropletSuite) TestCatalog_TheSyncAnnouncesEveryRegionAndPlan() {
	s.clearOutbox()

	s.sync()

	regions := s.outboxPayloads(constants.CatalogRegionChanged)
	s.Require().NotEmpty(regions)
	var vno1 map[string]any
	for _, r := range regions {
		if r["code"] == "vno1" {
			vno1 = r
		}
	}
	s.Require().NotNil(vno1)
	s.Equal("LT", vno1["country"])
	s.Equal(true, vno1["enabled"])
	s.isRFC3339UTC(vno1["synced_at"])

	plans := s.outboxPayloads(constants.CatalogPlanChanged)
	s.Require().NotEmpty(plans)
	var c432 map[string]any
	for _, p := range plans {
		if p["code"] == "c4-32" && p["region_code"] == "vno1" {
			c432 = p
		}
	}
	s.Require().NotNil(c432, "every Plan of every Region goes out")
	s.Equal(float64(4), c432["cpu_count"])
	s.Equal(float64(32), c432["ram_gb"])
	s.Equal(false, c432["sold_out"])
	s.Equal(true, c432["enabled"])
	s.NotEmpty(c432["hourly_price"])
	s.NotEmpty(c432["monthly_price"])
}
