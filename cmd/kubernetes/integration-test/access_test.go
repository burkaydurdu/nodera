package integrationtest

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
)

func (s *kubernetesSuite) TestAccess_AStrangerFindsNoProjectAndAFinanceMemberOnlyReads() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	stranger := s.signIn()
	finance := s.member(p.TeamID, "finance")

	s.expectError(s.manage(http.MethodGet, projectPath(p, ""), "", stranger), http.StatusNotFound, 70, 10001)
	s.expectError(s.manage(http.MethodGet, clusterPath(p, id, ""), "", stranger), http.StatusNotFound, 70, 10001)
	s.expectError(s.manage(http.MethodGet, "/v1/projects/"+uuid.NewString()+"/clusters", "", p.Owner), http.StatusNotFound, 70, 10001)

	s.Equal(http.StatusOK, s.manage(http.MethodGet, projectPath(p, ""), "", finance).Code)
	s.Equal(http.StatusOK, s.manage(http.MethodGet, clusterPath(p, id, "/nodes"), "", finance).Code)
	s.expectError(s.open(project{TeamID: p.TeamID, ProjectID: p.ProjectID, Owner: finance}, "x"), http.StatusForbidden, 70, 10004)
	s.expectError(s.manage(http.MethodDelete, clusterPath(p, id, ""), "", finance), http.StatusForbidden, 70, 10004)
}

func (s *kubernetesSuite) TestAccess_ARemovedMemberLosesTheProject() {
	p := s.grantProject()
	id, _ := s.ready(p, "prod")
	developer := s.member(p.TeamID, "developer")
	s.Equal(http.StatusOK, s.manage(http.MethodGet, clusterPath(p, id, ""), "", developer).Code)

	s.consume(constants.TeamMemberRemoved, `{"payload":{"team_id":"`+p.TeamID.String()+`","user_id":"`+developer.ID.String()+`","removed_at":"`+time.Now().UTC().Format(time.RFC3339Nano)+`"}}`)

	s.expectError(s.manage(http.MethodGet, clusterPath(p, id, ""), "", developer), http.StatusNotFound, 70, 10001)
	s.Equal(http.StatusUnauthorized, serve(s.management, http.MethodGet, projectPath(p, ""), "", nil).Code, "no token, no answer")
}
