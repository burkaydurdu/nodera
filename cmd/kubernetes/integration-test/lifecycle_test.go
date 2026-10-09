package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
)

// The Control plane comes up in steps the provisioner repeats: asked for,
// ready, prepared; only then are the Nodes asked of the droplet context.
func (s *kubernetesSuite) TestProvision_RequestsTheNodesOnceTheControlPlaneCanBeJoined() {
	p := s.grantProject()
	id := s.opened(p, "prod").str("id")

	s.provision()
	s.Equal("provisioning", s.get(p, id).str("status"))
	s.Empty(s.outboxPayloads(constants.NodeRequested), "the Control plane is not ready yet")

	s.engine.Advance()
	s.provision()

	plane, _ := s.engine.Plane(uuid.MustParse(id))
	s.True(plane.Prepared, "the network and the admins' binding are installed before a Node joins")
	s.Len(plane.Joins, 2, "one join secret per Node")
	requests := s.outboxPayloads(constants.NodeRequested)
	s.Require().Len(requests, 2)
	for _, req := range requests {
		s.Equal(id, req["cluster_id"])
		s.Equal("workers", req["pool"])
		s.Equal(p.ProjectID.String(), req["project_id"])
		s.Equal(p.TeamID.String(), req["team_id"])
		s.Equal("c4-32", req["plan"])
		s.Equal(region, req["region"])
		s.Equal("ubuntu-24-04", req["image"])
		s.NotEmpty(s.userData(fmt.Sprint(req["node_id"])), "the cloud-init waits in Redis for the droplet context")
	}

	s.provision()
	s.Len(s.outboxPayloads(constants.NodeRequested), 2, "a Node is asked for once")
}

func (s *kubernetesSuite) TestProvision_AnEngineThatCannotBeReachedIsAskedAgain() {
	p := s.grantProject()
	s.engine.Fail(fmt.Errorf("%w: connection refused", errUnavailable()))
	id := s.opened(p, "prod").str("id")
	_, asked := s.engine.Plane(uuid.MustParse(id))
	s.False(asked, "the engine took nothing; the Cluster is recorded all the same")

	s.engine.Reset()
	s.provision()
	_, asked = s.engine.Plane(uuid.MustParse(id))
	s.True(asked, "the provisioner asks again")
}

func (s *kubernetesSuite) TestProvision_AControlPlaneNotReadyInTimeFailsTheCluster() {
	p := s.grantProject()
	id := s.opened(p, "prod").str("id")
	s.clock.Advance(31 * time.Minute)

	s.provision()

	r := s.get(p, id)
	s.Equal("failed", r.str("status"))
	s.Equal("control_plane_failed", r.str("failure_code"))
	s.Equal("the control plane was not ready in time", r.str("failure_reason"))
	changes := s.statusChanges(id)
	s.Equal("failed", changes[len(changes)-1]["status"])
	s.Equal("control_plane_failed", changes[len(changes)-1]["failure_code"])
}

func (s *kubernetesSuite) TestNodes_ADropletBecomingActiveIsANodeJoining() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	dropletID := uuid.NewString()

	s.dropletChanged(fmt.Sprint(nodes[0]["id"]), id, dropletID, "provisioning", "", nil)
	s.Equal("requested", s.nodes(p, id)[0]["status"], "a Droplet being ordered is no Node yet")

	s.dropletChanged(fmt.Sprint(nodes[0]["id"]), id, dropletID, "active", "203.0.113.7", nil)
	n := s.nodes(p, id)[0]
	s.Equal("joining", n["status"])
	s.Equal(dropletID, n["droplet_id"])
	s.Equal("active", n["droplet_status"])
	s.Equal("203.0.113.7", n["ipv4"])
	s.Contains(s.realtimeTypes(), constants.RealtimeClusterChanged)
}

func (s *kubernetesSuite) TestNodes_TheClusterRunsWhenAKubeletReportsReady() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	s.dropletChanged(fmt.Sprint(nodes[0]["id"]), id, uuid.NewString(), "active", "203.0.113.7", nil)
	s.engine.Join(uuid.MustParse(id), fmt.Sprint(nodes[0]["hostname"]), "nodera://"+fmt.Sprint(nodes[0]["id"]), false)

	s.watch()
	s.Equal("joining", s.nodes(p, id)[0]["status"], "registered, but the kubelet is not ready yet: still joining")
	s.Equal("provisioning", s.get(p, id).str("status"))

	s.engine.Join(uuid.MustParse(id), fmt.Sprint(nodes[0]["hostname"]), "nodera://"+fmt.Sprint(nodes[0]["id"]), true)
	s.watch()
	n := s.nodes(p, id)[0]
	s.Equal("ready", n["status"])
	s.NotNil(n["ready_at"])
	s.Equal("running", s.get(p, id).str("status"))
	changes := s.statusChanges(id)
	s.Equal("provisioning", changes[len(changes)-1]["from_status"])
	s.Equal("running", changes[len(changes)-1]["status"])

	s.engine.Join(uuid.MustParse(id), fmt.Sprint(nodes[0]["hostname"]), "nodera://"+fmt.Sprint(nodes[0]["id"]), false)
	s.watch()
	s.Equal("degraded", s.get(p, id).str("status"), "no Node ready: the API server answers to nobody")

	s.engine.Join(uuid.MustParse(id), fmt.Sprint(nodes[0]["hostname"]), "nodera://"+fmt.Sprint(nodes[0]["id"]), true)
	s.watch()
	s.Equal("running", s.get(p, id).str("status"))
}

func (s *kubernetesSuite) TestNodes_ANodeThatRegistersButNeverBecomesReadyFailsToo() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	nodeID := fmt.Sprint(nodes[0]["id"])
	s.dropletChanged(nodeID, id, uuid.NewString(), "active", "203.0.113.7", nil)
	s.engine.Join(uuid.MustParse(id), fmt.Sprint(nodes[0]["hostname"]), "nodera://"+nodeID, false)

	s.clock.Advance(16 * time.Minute)
	s.watch()

	n := s.nodes(p, id)[0]
	s.Equal("failed", n["status"], "a kubelet whose network never comes up is a Node that never joined")
	s.Equal("timed_out", n["failure_code"])
}

// A Node released while its Droplet was still being asked for may have its
// Droplet opened after the release reached the droplet context; the Droplet
// turning up is given back again rather than left running for nobody.
func (s *kubernetesSuite) TestNodes_ADropletThatTurnsUpAfterTheReleaseIsGivenBackAgain() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	newest := nodes[1]
	s.Require().Equal(http.StatusOK, s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+s.poolID(p, id, 0)+"/count"), `{"count":1}`, p.Owner).Code)
	s.release()
	s.Require().Len(s.outboxPayloads(constants.NodeReleased), 1, "released without a Droplet to drain")

	s.dropletChanged(fmt.Sprint(newest["id"]), id, uuid.NewString(), "active", "203.0.113.9", nil)
	s.release()

	s.Len(s.outboxPayloads(constants.NodeReleased), 2, "the Droplet that came late is given back")
	s.dropletChanged(fmt.Sprint(newest["id"]), id, fmt.Sprint(s.nodes(p, id)[1]["droplet_id"]), "deleted", "", nil)
	s.Len(s.nodes(p, id), 1)
}

func (s *kubernetesSuite) TestNodes_ARejectionOfAReleasingNodeEndsItAtOnce() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	newest := nodes[1]
	s.Require().Equal(http.StatusOK, s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+s.poolID(p, id, 0)+"/count"), `{"count":1}`, p.Owner).Code)
	s.release()

	s.nodeRejected(fmt.Sprint(newest["id"]), id, "payment method required")
	s.release()

	s.Len(s.nodes(p, id), 1, "nothing to wait for: the Node is gone")
}

func (s *kubernetesSuite) TestNodes_ANodeThatNeverJoinsFailsAndItsDropletIsGivenBack() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	nodeID := fmt.Sprint(nodes[0]["id"])
	s.dropletChanged(nodeID, id, uuid.NewString(), "active", "203.0.113.7", nil)

	s.clock.Advance(14 * time.Minute)
	s.watch()
	s.Equal("joining", s.nodes(p, id)[0]["status"], "still within the time a boot takes")

	s.clock.Advance(2 * time.Minute)
	s.watch()
	n := s.nodes(p, id)[0]
	s.Equal("failed", n["status"])
	s.Equal("timed_out", n["failure_code"])
	s.Equal("the node did not join the cluster in time", n["failure_reason"])

	s.release()
	released := s.outboxPayloads(constants.NodeReleased)
	s.Require().Len(released, 1, "the Droplet of a Node that failed is given back")
	s.Equal(nodeID, released[0]["node_id"])

	s.dropletChanged(nodeID, id, fmt.Sprint(n["droplet_id"]), "deleted", "", nil)
	s.Len(s.nodes(p, id), 1, "a gone Node is not shown")
	s.NotEqual(nodeID, s.nodes(p, id)[0]["id"])
}

func (s *kubernetesSuite) TestNodes_ARejectedRequestFailsTheNodeWithTheDropletContextsReason() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")

	s.nodeRejected(fmt.Sprint(nodes[0]["id"]), id, "payment method required")
	s.nodeRejected(fmt.Sprint(nodes[1]["id"]), id, "plan sold out")

	for _, n := range s.nodes(p, id) {
		s.Equal("failed", n["status"])
		s.Equal("node_rejected", n["failure_code"])
	}
	s.Equal("payment method required", s.nodes(p, id)[0]["failure_reason"])
	s.Equal("degraded", s.get(p, id).str("status"), "every first Node failed: the Control plane answers to nobody")
	s.release()
	s.Empty(s.outboxPayloads(constants.NodeReleased), "a Node that never had a Droplet has nothing to give back")
}

// The count a pool asks for counts only standing Nodes, so choosing it again
// replaces the ones that failed, and those that never had a Droplet go.
func (s *kubernetesSuite) TestNodes_ChoosingTheCountAgainReplacesFailedNodes() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	for _, n := range nodes {
		s.nodeRejected(fmt.Sprint(n["id"]), id, "payment method required")
	}
	s.clearOutbox()

	r := s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+s.poolID(p, id, 0)+"/count"), `{"count":2}`, p.Owner)

	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	listed := s.nodes(p, id)
	s.Require().Len(listed, 2, "the failed Nodes are replaced, not kept beside their replacements")
	for _, n := range listed {
		s.Equal("requested", n["status"])
		s.NotEqual(nodes[0]["id"], n["id"])
		s.NotEqual(nodes[1]["id"], n["id"])
	}
	s.Len(s.outboxPayloads(constants.NodeRequested), 2)
}

func (s *kubernetesSuite) TestNodes_AFailedDropletFailsTheNode() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")

	s.dropletChanged(fmt.Sprint(nodes[0]["id"]), id, uuid.NewString(), "failed", "", map[string]any{"failure_code": "rejected", "failure_reason": "the server request was refused"})

	n := s.nodes(p, id)[0]
	s.Equal("failed", n["status"])
	s.Equal("node_rejected", n["failure_code"])
	s.Equal("the server request was refused", n["failure_reason"])
}

func (s *kubernetesSuite) TestNodes_AnEventForANodeThisContextDoesNotKnowIsAcknowledged() {
	s.dropletChanged(uuid.NewString(), uuid.NewString(), uuid.NewString(), "active", "", nil)
	s.nodeRejected(uuid.NewString(), uuid.NewString(), "whatever")
	s.Error(s.consumeErr(constants.DropletStatusChanged, `{"payload":{"node_id":"`+uuid.NewString()+`","status":"active"}}`), "a status without a Droplet id or its time is malformed")
}
