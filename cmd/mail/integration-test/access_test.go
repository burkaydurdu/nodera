package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/mail/async/constants"
)

func (s *mailSuite) TestAccess_RolesAndStrangers() {
	p := s.grantProject()
	finance := s.member(p.TeamID, "finance")
	developer := s.member(p.TeamID, "developer")
	stranger := s.signIn()

	s.Equal(http.StatusUnauthorized, serve(s.management, http.MethodGet, projectPath(p, ""), "", nil).Code)
	s.expectError(s.manage(http.MethodGet, projectPath(p, ""), "", stranger), http.StatusNotFound, 50, 10001)

	s.Equal(http.StatusOK, s.manage(http.MethodGet, projectPath(p, ""), "", finance).Code)
	s.Equal(http.StatusOK, s.manage(http.MethodGet, projectPath(p, "/domains"), "", finance).Code)
	s.expectError(s.manage(http.MethodPut, projectPath(p, "/package"), `{"code":"10k"}`, finance), http.StatusForbidden, 50, 10004)
	s.expectError(s.manage(http.MethodPost, projectPath(p, "/domains"), `{"name":"example.com"}`, finance), http.StatusForbidden, 50, 10004)

	s.Equal(http.StatusOK, s.manage(http.MethodPut, projectPath(p, "/package"), `{"code":"10k"}`, developer).Code)
	s.Equal(http.StatusCreated, s.manage(http.MethodPost, projectPath(p, "/domains"), `{"name":"example.com"}`, developer).Code)
}

func (s *mailSuite) TestAccess_ARemovedMemberLosesMail() {
	p := s.grantProject()
	developer := s.member(p.TeamID, "developer")
	s.Equal(http.StatusOK, s.manage(http.MethodGet, projectPath(p, ""), "", developer).Code)

	s.consume(constants.TeamMemberRemoved, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"removed_at":%q}}`,
		p.TeamID, developer.ID, time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)))
	s.expectError(s.manage(http.MethodGet, projectPath(p, ""), "", developer), http.StatusNotFound, 50, 10001)
}
