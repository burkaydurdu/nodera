package integrationtest

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
)

func (s *kubernetesSuite) TestOpen_RecordsTheClusterAndAsksForItsControlPlane() {
	p := s.grantProject()

	r := s.opened(p, "prod")

	s.Equal("provisioning", r.str("status"))
	s.Equal("prod", r.str("name"))
	s.Equal(region, r.str("region"))
	s.Equal("1.37.0", r.str("version"))
	s.Equal(r.str("id")+"."+region+"."+domain+":20000", r.str("endpoint"), "the Endpoint carries the Cluster id, the Region and a port of its own, and never changes")
	s.Equal("active", r.str("billing_status"))
	pools := r.list("node_pools")
	s.Require().Len(pools, 1)
	s.Equal("workers", pools[0]["name"])
	s.Equal("c4-32", pools[0]["plan_code"])
	s.Equal(float64(2), pools[0]["count"])

	nodes := s.nodes(p, r.str("id"))
	s.Require().Len(nodes, 2)
	for _, n := range nodes {
		s.Equal("requested", n["status"])
		s.True(strings.HasPrefix(n["hostname"].(string), "workers-"), n["hostname"])
		s.Nil(n["droplet_id"], "no Droplet is asked for before the Control plane can be joined")
	}
	s.Empty(s.outboxPayloads(constants.NodeRequested))

	plane, ok := s.engine.Plane(uuid.MustParse(r.str("id")))
	s.Require().True(ok, "the engine was asked for the Control plane at once")
	s.Equal("1.37.0", plane.Spec.Version)
	s.Equal(r.str("id")+"."+region+"."+domain, plane.Spec.Hostname)
	s.Equal(20000, plane.Spec.Port)
	s.Equal(20001, plane.Spec.TunnelPort, "the tunnel takes the port after the Endpoint's")

	changes := s.statusChanges(r.str("id"))
	s.Require().Len(changes, 1)
	s.Nil(changes[0]["from_status"])
	s.Equal("provisioning", changes[0]["status"])
	s.Equal("Shop", changes[0]["project_name"])
	s.Contains(s.realtimeTypes(), constants.RealtimeClusterChanged)
}

func (s *kubernetesSuite) TestOpen_IsListedAndShown() {
	p := s.grantProject()
	id := s.opened(p, "prod").str("id")
	s.opened(p, "staging")

	list := s.manage(http.MethodGet, projectPath(p, ""), "", p.Owner)
	s.Require().Equal(http.StatusOK, list.Code, list.Raw)
	s.Len(list.List, 2)
	s.Equal("prod", s.get(p, id).str("name"))
}

func (s *kubernetesSuite) TestOpen_RefusesWhatTheCatalogDoesNotSell() {
	p := s.grantProject()

	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"name":"Prod_1","region":"vno1","version":"1.37.0","node_pool":{"name":"workers","plan":"c4-32","count":1}}`, p.Owner), http.StatusBadRequest, 73, 10002)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"name":"prod","region":"ams1","version":"1.37.0","node_pool":{"name":"workers","plan":"c4-32","count":1}}`, p.Owner), http.StatusBadRequest, 72, 10005)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"name":"prod","region":"nowhere","version":"1.37.0","node_pool":{"name":"workers","plan":"c4-32","count":1}}`, p.Owner), http.StatusNotFound, 72, 10001)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"name":"prod","region":"vno1","version":"1.30.0","node_pool":{"name":"workers","plan":"c4-32","count":1}}`, p.Owner), http.StatusBadRequest, 72, 10006)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), openBody("prod", "c1-4", 1), p.Owner), http.StatusBadRequest, 72, 10003)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), openBody("prod", "c2-16", 1), p.Owner), http.StatusConflict, 72, 10004)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), openBody("prod", "no-such", 1), p.Owner), http.StatusNotFound, 72, 10002)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), openBody("prod", "c4-32", 6), p.Owner), http.StatusBadRequest, 73, 10009)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"name":"prod","region":"vno1","version":"1.37.0","node_pool":{"name":"Workers Pool","plan":"c4-32","count":1}}`, p.Owner), http.StatusBadRequest, 73, 10006)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"name":"prod","region":"vno1","version":"1.37.0","node_pool":{"name":"workers","plan":"c4-32","count":1,"labels":{"bad key!":"x"}}}`, p.Owner), http.StatusBadRequest, 73, 10011)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"name":"prod","region":"vno1","version":"1.37.0","node_pool":{"name":"workers","plan":"c4-32","count":1,"taints":[{"key":"gpu","value":"true","effect":"Maybe"}]}}`, p.Owner), http.StatusBadRequest, 73, 10012)
	s.Empty(s.engine.Calls(), "nothing reached the engine")
}

func (s *kubernetesSuite) TestOpen_NameIsUniqueInTheProjectUntilDeleted() {
	p := s.grantProject()
	id := s.opened(p, "prod").str("id")

	s.expectError(s.open(p, "prod"), http.StatusConflict, 73, 10003)

	other := s.grantProject()
	s.Equal(http.StatusCreated, s.open(other, "prod").Code, "another Project may use the name")

	s.Require().Equal(http.StatusOK, s.manage(http.MethodDelete, clusterPath(p, id, ""), "", p.Owner).Code)
	s.expectError(s.open(p, "prod"), http.StatusConflict, 73, 10003, "a Cluster still deleting keeps its name")
}

func (s *kubernetesSuite) TestRename_ChangesTheNameAndNothingElse() {
	p := s.grantProject()
	r := s.opened(p, "prod")
	id := r.str("id")

	renamed := s.manage(http.MethodPut, clusterPath(p, id, "/name"), `{"name":"prod-eu"}`, p.Owner)
	s.Require().Equal(http.StatusOK, renamed.Code, renamed.Raw)
	s.Equal("prod-eu", renamed.str("name"))
	s.Equal(r.str("endpoint"), renamed.str("endpoint"), "the Endpoint carries the id, never the name")

	second := s.open(p, "prod")
	s.Equal(http.StatusCreated, second.Code, "the old name is free")
	s.expectError(s.manage(http.MethodPut, clusterPath(p, second.str("id"), "/name"), `{"name":"prod-eu"}`, p.Owner), http.StatusConflict, 73, 10003)
	s.expectError(s.manage(http.MethodPut, clusterPath(p, id, "/name"), `{"name":"Prod EU"}`, p.Owner), http.StatusBadRequest, 73, 10002)
	s.expectError(s.manage(http.MethodPut, clusterPath(p, id, "/name"), `{"name":"x"}`, s.member(p.TeamID, "finance")), http.StatusForbidden, 70, 10004)
}

func (s *kubernetesSuite) TestOpen_AProjectHoldsAsManyClustersAsItMay() {
	p := s.grantProject()
	s.opened(p, "a")
	s.opened(p, "b")

	s.expectError(s.open(p, "c"), http.StatusConflict, 73, 10004)
}

func (s *kubernetesSuite) TestOpen_LabelsAndTaintsReachTheNodesCloudInit() {
	p := s.grantProject()
	body := `{"name":"prod","region":"vno1","version":"1.37.0","node_pool":{"name":"gpu","plan":"c8-64","count":1,"labels":{"tier":"gpu"},"taints":[{"key":"gpu","value":"true","effect":"NoSchedule"}]}}`
	r := s.manage(http.MethodPost, projectPath(p, ""), body, p.Owner)
	s.Require().Equal(http.StatusCreated, r.Code, r.Raw)
	s.engine.Advance()
	s.provision()

	nodes := s.nodes(p, r.str("id"))
	s.Require().Len(nodes, 1)
	userData := s.userData(fmt.Sprint(nodes[0]["id"]))
	s.Contains(userData, "--node-labels=nodera.cloud/pool=gpu,tier=gpu")
	s.Contains(userData, "--register-with-taints=gpu=true:NoSchedule")
	s.Contains(userData, "--provider-id=nodera://"+fmt.Sprint(nodes[0]["id"]))
	s.Contains(userData, "kubeadm join "+r.str("endpoint")+" --token ")
	s.Contains(userData, "--discovery-token-ca-cert-hash sha256:")
	s.Contains(userData, "kubelet=1.37.0-* kubeadm=1.37.0-*")
}

// A pod in a Cluster reaches its API server by address alone, so no two
// Clusters of a Region share a port; each takes two, the Endpoint's and its
// tunnel's (docs/adr/0015).
func (s *kubernetesSuite) TestOpen_EveryClusterOfTheRegionAnswersOnPortsOfItsOwn() {
	first, second := s.grantProject(), s.grantProject()

	a := s.opened(first, "prod")
	b := s.opened(second, "prod")

	s.True(strings.HasSuffix(a.str("endpoint"), ":20000"), a.str("endpoint"))
	s.True(strings.HasSuffix(b.str("endpoint"), ":20002"), b.str("endpoint"))
	plane, _ := s.engine.Plane(uuid.MustParse(b.str("id")))
	s.Equal(20002, plane.Spec.Port)
	s.Equal(20003, plane.Spec.TunnelPort)
}

// A Region whose ports are all held opens no Cluster until one is deleted,
// and the next one opened takes the port the deleted one held.
func (s *kubernetesSuite) TestOpen_ARegionWithNoPortLeftOpensNoClusterUntilOneIsDeleted() {
	projects := []project{s.grantProject(), s.grantProject(), s.grantProject()}
	ids := make([]string, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, s.opened(p, "prod").str("id"))
	}
	late := s.grantProject()

	s.expectError(s.open(late, "prod"), http.StatusServiceUnavailable, 73, 10022)

	s.Require().Equal(http.StatusOK, s.manage(http.MethodDelete, clusterPath(projects[0], ids[0], ""), "", projects[0].Owner).Code)
	s.finish()
	r := s.opened(late, "prod")
	s.True(strings.HasSuffix(r.str("endpoint"), ":20000"), "the deleted Cluster's port, "+r.str("endpoint"))
}
