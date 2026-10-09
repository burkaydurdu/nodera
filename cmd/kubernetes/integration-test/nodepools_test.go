package integrationtest

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
)

func (s *kubernetesSuite) poolID(p project, id string, index int) string {
	return fmt.Sprint(s.get(p, id).list("node_pools")[index]["id"])
}

func (s *kubernetesSuite) TestNodePools_ResizingUpAsksForNodesAtOnceWhenTheClusterCanBeJoined() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	s.clearOutbox()

	r := s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+s.poolID(p, id, 0)+"/count"), `{"count":4}`, p.Owner)

	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	s.Equal(float64(4), r.list("node_pools")[0]["count"])
	s.Len(s.nodes(p, id), 4)
	s.Len(s.outboxPayloads(constants.NodeRequested), 2, "the two new Nodes are asked for without waiting for a job")
}

func (s *kubernetesSuite) TestNodePools_ResizingDownReleasesTheNewestNodes() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	s.joined(p, id, nodes)
	s.clearOutbox()

	r := s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+s.poolID(p, id, 0)+"/count"), `{"count":1}`, p.Owner)

	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	listed := s.nodes(p, id)
	s.Require().Len(listed, 2)
	s.Equal("ready", listed[0]["status"], "the oldest stays")
	s.Equal("releasing", listed[1]["status"], "the newest goes")

	s.release()
	released := s.outboxPayloads(constants.NodeReleased)
	s.Require().Len(released, 1)
	s.Equal(listed[1]["id"], released[0]["node_id"])
	plane, _ := s.engine.Plane(uuid.MustParse(id))
	_, stillMember := plane.Nodes[fmt.Sprint(listed[1]["hostname"])]
	s.False(stillMember, "the Cluster forgot the Node")

	s.dropletChanged(fmt.Sprint(listed[1]["id"]), id, fmt.Sprint(listed[1]["droplet_id"]), "deleted", "", nil)
	s.Len(s.nodes(p, id), 1)
	s.Equal("running", s.get(p, id).str("status"))
}

func (s *kubernetesSuite) TestNodePools_ADrainWaitsForTheWorkloadsThenForces() {
	p := s.grantProject()
	id, nodes := s.ready(p, "prod")
	s.joined(p, id, nodes)
	newest := s.nodes(p, id)[1]
	s.engine.SetPods(uuid.MustParse(id), fmt.Sprint(newest["hostname"]), 3)
	s.Require().Equal(http.StatusOK, s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+s.poolID(p, id, 0)+"/count"), `{"count":1}`, p.Owner).Code)

	s.release()
	s.Empty(s.outboxPayloads(constants.NodeReleased), "workloads are still leaving")
	plane, _ := s.engine.Plane(uuid.MustParse(id))
	s.True(plane.Nodes[fmt.Sprint(newest["hostname"])].Cordoned)
	s.Equal(2, plane.Nodes[fmt.Sprint(newest["hostname"])].Pods)

	s.clock.Advance(6 * 60 * 1e9)
	s.release()
	s.Len(s.outboxPayloads(constants.NodeReleased), 1, "past the grace the drain is forced")
	s.Contains(s.engine.Calls(), fmt.Sprintf("DrainNode %s %s force=true", id, newest["hostname"]))
}

func (s *kubernetesSuite) TestNodePools_ResizeOutOfTheLimitsIsRefused() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	pool := s.poolID(p, id, 0)

	s.Equal(http.StatusBadRequest, s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+pool+"/count"), `{"count":0}`, p.Owner).Code)
	s.expectError(s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+pool+"/count"), `{"count":6}`, p.Owner), http.StatusBadRequest, 73, 10009)
	s.expectError(s.manage(http.MethodPut, clusterPath(p, id, "/node-pools/"+uuid.NewString()+"/count"), `{"count":2}`, p.Owner), http.StatusNotFound, 73, 10005)
}

func (s *kubernetesSuite) TestNodePools_AddingAPoolOpensItsNodes() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	s.clearOutbox()

	r := s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"batch","plan":"c8-64","count":1,"labels":{"tier":"batch"}}`, p.Owner)

	s.Require().Equal(http.StatusCreated, r.Code, r.Raw)
	pools := r.list("node_pools")
	s.Require().Len(pools, 2)
	s.Equal("batch", pools[1]["name"])
	s.Equal(map[string]any{"tier": "batch"}, pools[1]["labels"])
	s.Len(s.nodes(p, id), 3)
	requests := s.outboxPayloads(constants.NodeRequested)
	s.Require().Len(requests, 1)
	s.Equal("c8-64", requests[0]["plan"])
	s.Equal("batch", requests[0]["pool"])
}

func (s *kubernetesSuite) TestNodePools_AddRefusals() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")

	s.expectError(s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"workers","plan":"c8-64","count":1}`, p.Owner), http.StatusConflict, 73, 10007)
	s.expectError(s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"small","plan":"c1-4","count":1}`, p.Owner), http.StatusBadRequest, 72, 10003)
	s.Require().Equal(http.StatusCreated, s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"batch","plan":"c8-64","count":1}`, p.Owner).Code)
	s.expectError(s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"more","plan":"c8-64","count":1}`, p.Owner), http.StatusConflict, 73, 10008)
}

func (s *kubernetesSuite) TestNodePools_DeletingAPoolReleasesItsNodesAndTheLastOneStays() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	s.Require().Equal(http.StatusCreated, s.manage(http.MethodPost, clusterPath(p, id, "/node-pools"), `{"name":"batch","plan":"c8-64","count":1}`, p.Owner).Code)
	batch := s.poolID(p, id, 1)

	r := s.manage(http.MethodDelete, clusterPath(p, id, "/node-pools/"+batch), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	s.Len(r.list("node_pools"), 1)
	var releasing int
	for _, n := range s.nodes(p, id) {
		if n["status"] == "releasing" {
			releasing++
		}
	}
	s.Equal(1, releasing)

	s.expectError(s.manage(http.MethodDelete, clusterPath(p, id, "/node-pools/"+s.poolID(p, id, 0)), "", p.Owner), http.StatusConflict, 73, 10010)
}
