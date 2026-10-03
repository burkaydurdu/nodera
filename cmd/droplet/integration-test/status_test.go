package integrationtest

import (
	"net/http"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/pkg/realtime"
)

func (s *dropletSuite) TestStatus_BecomingActiveIsAnnounced() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")

	changes := s.statusChanges(id)
	s.Require().Len(changes, 2, "created as provisioning, then active")
	s.Nil(changes[0]["from_status"], "a new Droplet comes from nowhere")
	s.Equal("provisioning", changes[0]["status"])

	active := changes[1]
	s.Equal("provisioning", active["from_status"])
	s.Equal("active", active["status"])
	s.Equal(teamID.String(), active["team_id"])
	s.Equal(projectID.String(), active["project_id"])
	s.Equal("web-1", active["hostname"])
	s.Equal(s.get("/v1/droplets/"+id, u.Token).str("ipv4"), active["ipv4"])
	s.NotEmpty(active["ipv4"])
	s.isRFC3339UTC(active["changed_at"])
}

func (s *dropletSuite) TestStatus_TheTeamIsToldLive() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")

	live := s.liveMessages("droplet.status_changed")
	s.Require().Len(live, 2)
	address, _ := live[1]["address"].(map[string]any)
	s.Equal(teamID.String(), address["team_id"])
	s.Equal("droplet.read", address["permission"], "whoever may see the Droplet sees it change")
	s.Nil(address["user_id"])
	data, _ := live[1]["data"].(map[string]any)
	s.Equal(map[string]any{"team_id": teamID.String(), "droplet_id": id, "project_id": projectID.String(), "status": "active"}, data)
}

func (s *dropletSuite) TestStatus_AProvisioningFailureCarriesTheCustomerSafeReason() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.FailCreate(provider.ErrorRejected)
	created := s.createDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)

	changes := s.statusChanges(created.str("id"))
	s.Require().Len(changes, 2)
	s.Equal("failed", changes[1]["status"])
	s.Equal(provider.ErrorRejected.Message, changes[1]["failure_reason"])
	s.Equal("rejected", changes[1]["failure_code"])
}

func (s *dropletSuite) TestStatus_AServerGoneAtTheProviderFailsTheDropletWithItsCode() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusAccepted, s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"power_off"}`, u.Token).Code)
	s.fake.FailGet(provider.ErrorServerNotFound)
	defer s.fake.FailGet(nil)

	s.poll()

	detail := s.get("/v1/droplets/"+id, u.Token)
	s.Equal("failed", detail.str("status"))
	s.Equal("server_gone", detail.str("failure_code"))
	changes := s.statusChanges(id)
	s.Equal("server_gone", changes[len(changes)-1]["failure_code"])
}

func (s *dropletSuite) TestStatus_OnlyARealChangeIsAnnounced() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	s.clearOutbox()

	s.Require().Equal(http.StatusAccepted, s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"power_off"}`, u.Token).Code)
	s.poll()
	s.Empty(s.statusChanges(id), "a power-off asked for and a poll the Provider has not caught up with change nothing")

	s.fake.Advance()
	s.poll()
	changes := s.statusChanges(id)
	s.Require().Len(changes, 1)
	s.Equal("active", changes[0]["from_status"])
	s.Equal("off", changes[0]["status"])
}

// statusChanges answers the Droplet's DropletStatusChanged payloads in order.
func (s *dropletSuite) statusChanges(dropletID string) []map[string]any {
	changes := make([]map[string]any, 0)
	for _, payload := range s.outboxPayloads(constants.DropletStatusChanged) {
		if payload["droplet_id"] == dropletID {
			changes = append(changes, payload)
		}
	}

	return changes
}

// liveMessages answers the realtime messages of one type, in order.
func (s *dropletSuite) liveMessages(messageType string) []map[string]any {
	messages := make([]map[string]any, 0)
	for _, payload := range s.outboxPayloads(realtime.RoutingKey) {
		if payload["type"] == messageType {
			messages = append(messages, payload)
		}
	}

	return messages
}
