package integrationtest

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
)

// Deleting a Cluster releases every Node without a drain, waits for their
// Droplets to be deleted and then removes the Control plane.
func (s *kubernetesSuite) TestDelete_ReleasesTheNodesThenRemovesTheControlPlane() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	s.joined(p, id, nodes)
	s.clearOutbox()

	r := s.manage(http.MethodDelete, clusterPath(p, id, ""), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	s.Equal("deleting", r.str("status"))
	for _, n := range s.nodes(p, id) {
		s.Equal("releasing", n["status"])
	}
	changes := s.statusChanges(id)
	s.Equal("deleting", changes[len(changes)-1]["status"])

	s.release()
	s.Len(s.outboxPayloads(constants.NodeReleased), 2)
	s.NotContains(fmt.Sprint(s.engine.Calls()), "DrainNode", "a deleting Cluster's Nodes are not drained")

	s.finish()
	s.Equal("deleting", s.get(p, id).str("status"), "the Droplets are not deleted yet")
	_, held := s.engine.Plane(uuid.MustParse(id))
	s.True(held)

	for _, n := range s.nodes(p, id) {
		s.dropletChanged(fmt.Sprint(n["id"]), id, fmt.Sprint(n["droplet_id"]), "deleted", "", nil)
	}
	s.finish()
	_, held = s.engine.Plane(uuid.MustParse(id))
	s.False(held, "the Control plane is gone")
	s.Equal(http.StatusNotFound, s.manage(http.MethodGet, clusterPath(p, id, ""), "", p.Owner).Code)
	s.Empty(s.manage(http.MethodGet, projectPath(p, ""), "", p.Owner).List)
	changes = s.statusChanges(id)
	s.Equal("deleted", changes[len(changes)-1]["status"])

	s.Equal(http.StatusCreated, s.open(p, "prod").Code, "the name is free again")
}

func (s *kubernetesSuite) TestDelete_AClusterWhoseNodesNeverHadDropletsGoesAtOnce() {
	p := s.grantProject()
	id := s.opened(p, "prod").str("id")

	s.Require().Equal(http.StatusOK, s.manage(http.MethodDelete, clusterPath(p, id, ""), "", p.Owner).Code)
	s.Empty(s.nodes(p, id), "Nodes never asked for are gone at once")
	s.finish()

	s.Equal(http.StatusNotFound, s.manage(http.MethodGet, clusterPath(p, id, ""), "", p.Owner).Code)
}

func (s *kubernetesSuite) TestDelete_RefusesChangesWhileDeleting() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	pool := s.poolID(p, id, 0)
	s.Require().Equal(http.StatusOK, s.manage(http.MethodDelete, clusterPath(p, id, ""), "", p.Owner).Code)

	s.expectError(s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"batch","plan":"c8-64","count":1}`, p.Owner), http.StatusConflict, 73, 10013)
	s.expectError(s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+pool+"/count"), `{"count":3}`, p.Owner), http.StatusConflict, 73, 10013)
	s.expectError(s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", p.Owner), http.StatusConflict, 73, 10013)
	s.Equal(http.StatusOK, s.manage(http.MethodDelete, clusterPath(p, id, ""), "", p.Owner).Code, "deleting again changes nothing")
}

func (s *kubernetesSuite) TestDelete_AFailedClusterCanOnlyBeDeleted() {
	p := s.grantProject()
	id := s.opened(p, "prod").str("id")
	s.clock.Advance(31 * 60 * 1e9)
	s.provision()
	s.Require().Equal("failed", s.get(p, id).str("status"))

	s.expectError(s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"batch","plan":"c8-64","count":1}`, p.Owner), http.StatusConflict, 73, 10019)
	s.expectError(s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", p.Owner), http.StatusConflict, 73, 10019)
	s.Equal(http.StatusOK, s.manage(http.MethodDelete, clusterPath(p, id, ""), "", p.Owner).Code)
}
