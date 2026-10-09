package integrationtest

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

func (s *kubernetesSuite) TestKubeconfig_IsCutForTheCallerOnceTheControlPlaneIsReady() {
	p := s.grantProject()
	id := s.opened(p, "prod").str("id")

	s.expectError(s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", p.Owner), http.StatusConflict, 73, 10014)

	s.engine.Advance()
	s.provision()
	r := s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", p.Owner)

	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	s.Contains(r.str("kubeconfig"), "server: https://"+id+"."+region+"."+domain+":20000\n", "the Endpoint with the Cluster's own port")
	expires, err := time.Parse(time.RFC3339Nano, r.str("expires_at"))
	s.Require().NoError(err)
	s.WithinDuration(s.clock.Now().Add(7*24*time.Hour), expires, time.Second, "good for seven days")

	plane, _ := s.engine.Plane(uuid.MustParse(id))
	s.Require().Len(plane.Kubeconfigs, 1)
	s.Equal(p.Owner.ID.String(), plane.Kubeconfigs[0].User)
	s.Equal([]string{"nodera:cluster-admins"}, plane.Kubeconfigs[0].Groups, "an RBAC group of Nodera's, never system:masters")
}

func (s *kubernetesSuite) TestKubeconfig_NeedsKubernetesWrite() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	finance := s.member(p.TeamID, "finance")
	developer := s.member(p.TeamID, "developer")

	s.expectError(s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", finance), http.StatusForbidden, 70, 10004)
	s.Equal(http.StatusOK, s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", developer).Code)
}

func (s *kubernetesSuite) TestKubeconfig_AnEngineThatCannotBeReachedAnswersUnavailable() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	s.engine.Fail(errUnavailable())

	s.expectError(s.manage(http.MethodGet, clusterPath(p, id, "/kubeconfig"), "", p.Owner), http.StatusServiceUnavailable, 73, 10017)
}
