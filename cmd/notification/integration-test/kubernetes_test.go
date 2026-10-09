package integrationtest

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/email"
)

type kubernetesCluster struct {
	ID        uuid.UUID
	TeamID    uuid.UUID
	ProjectID uuid.UUID
	Name      string
}

func newCluster(teamID uuid.UUID, name string) kubernetesCluster {
	return kubernetesCluster{ID: uuid.New(), TeamID: teamID, ProjectID: uuid.New(), Name: name}
}

// clusterChanged is the kubernetes context publishing ClusterStatusChanged.
func (s *notificationSuite) clusterChanged(c kubernetesCluster, from *string, to string, extra map[string]any) error {
	payload := map[string]any{
		"cluster_id": c.ID, "team_id": c.TeamID, "project_id": c.ProjectID, "project_name": "Shop", "name": c.Name, "region": "vno1",
		"from_status": from, "status": to, "changed_at": time.Now().UTC(),
	}
	for k, v := range extra {
		payload[k] = v
	}

	return s.consumeErr(constants.ClusterStatusChanged, s.json(map[string]any{"payload": payload}))
}

func (s *notificationSuite) TestClusterRunning_ReachesTheInboxOfEveryMemberWhoMaySeeKubernetes() {
	ayse, mehmet, zeynep, stranger := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr"), s.signIn("Zeynep", "en"), s.signIn("Can", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")
	s.addMember(teamID, zeynep, "finance")
	s.team("Other", stranger)
	c := newCluster(teamID, "prod")
	provisioning := "provisioning"

	s.Require().NoError(s.clusterChanged(c, nil, "provisioning", nil))
	s.Require().NoError(s.clusterChanged(c, &provisioning, "running", nil))

	for _, member := range []user{ayse, mehmet, zeynep} {
		items := s.inbox(member)
		s.Require().Len(items, 1, member.FullName)
		s.Equal("kubernetes_cluster_running", items[0]["type"])
		s.Equal(map[string]any{
			"cluster_id": c.ID.String(), "project_id": c.ProjectID.String(), "project_name": "Shop", "name": "prod",
		}, items[0]["params"])
	}
	s.Empty(s.inbox(stranger), "another Team's Cluster is none of their business")
	s.Empty(s.email.sent, "a Cluster becoming ready is told in the app only")
}

func (s *notificationSuite) TestClusterRunning_IsToldOnceHoweverTheClusterGotThere() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	c := newCluster(teamID, "prod")
	provisioning, degraded, running := "provisioning", "degraded", "running"

	s.Require().NoError(s.clusterChanged(c, &provisioning, "degraded", nil))
	s.Require().NoError(s.clusterChanged(c, &degraded, "running", nil))
	s.Require().NoError(s.clusterChanged(c, &running, "degraded", nil))
	s.Require().NoError(s.clusterChanged(c, &degraded, "running", nil))

	s.Len(s.inbox(ayse), 1, "every first Node failed, a later one joined: told once, and never again")
}

func (s *notificationSuite) TestClusterRunning_ARedeliveredEventRecordsNothingTwice() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	c := newCluster(teamID, "prod")
	provisioning := "provisioning"

	s.Require().NoError(s.clusterChanged(c, &provisioning, "running", nil))
	s.Require().NoError(s.clusterChanged(c, &provisioning, "running", nil))

	s.Len(s.inbox(ayse), 1)
}

func (s *notificationSuite) TestClusterStatus_OtherMovesTellNobody() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	c := newCluster(teamID, "prod")
	running, deleting := "running", "deleting"

	s.Require().NoError(s.clusterChanged(c, nil, "provisioning", nil))
	s.Require().NoError(s.clusterChanged(c, &running, "degraded", nil))
	s.Require().NoError(s.clusterChanged(c, &running, "deleting", nil))
	s.Require().NoError(s.clusterChanged(c, &deleting, "deleted", nil))

	s.Empty(s.inbox(ayse), "a Cluster being opened, losing its Nodes or being deleted is no news")
}

func (s *notificationSuite) TestClusterFailed_GoesByEmailToo() {
	ayse, zeynep := s.signIn("Ayse", "tr"), s.signIn("Zeynep", "en")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, zeynep, "finance")
	c := newCluster(teamID, "prod")
	provisioning := "provisioning"

	s.Require().NoError(s.clusterChanged(c, &provisioning, "failed", map[string]any{"failure_code": "control_plane_failed", "failure_reason": "the control plane was not ready in time"}))

	items := s.inbox(ayse)
	s.Require().Len(items, 1)
	s.Equal("kubernetes_cluster_failed", items[0]["type"])
	s.Equal("control_plane_failed", items[0]["params"].(map[string]any)["failure_code"], "the web app words the reason from its code")

	sent := s.emailsTo(zeynep)
	s.Require().Len(sent, 1)
	s.Equal(email.KindKubernetesClusterFailed, sent[0].Kind)
	s.Equal("en", sent[0].Locale)
	s.Equal("prod", sent[0].Params["Name"])
	s.Equal("Shop", sent[0].Params["ProjectName"])
	s.Equal("kubernetes_failure_control_plane_failed", sent[0].Params["FailureKey"])
	s.Len(s.emailsTo(ayse), 1)
}

func (s *notificationSuite) TestClusterStatus_AMalformedEventIsRefused() {
	s.Error(s.consumeErr(constants.ClusterStatusChanged, `{"payload":{"status":"running"}}`))
	s.Error(s.consumeErr(constants.ClusterStatusChanged, `not json`))
}
