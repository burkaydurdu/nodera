package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
)

func (s *storageSuite) TestAccess_RolesAndStrangers() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	finance := s.member(p.TeamID, "finance")
	developer := s.member(p.TeamID, "developer")
	stranger := s.signIn()

	s.Equal(http.StatusUnauthorized, serve(s.management, http.MethodGet, projectPath(p, ""), "", nil).Code)
	s.expectError(s.manage(http.MethodGet, projectPath(p, ""), "", stranger), http.StatusNotFound, 60, 10001)
	s.expectError(s.manage(http.MethodGet, storagePath(p, id, ""), "", stranger), http.StatusNotFound, 60, 10001)

	s.Equal(http.StatusOK, s.manage(http.MethodGet, projectPath(p, ""), "", finance).Code)
	s.Equal(http.StatusOK, s.manage(http.MethodGet, storagePath(p, id, "/access-keys"), "", finance).Code)
	s.Equal(http.StatusOK, s.manage(http.MethodGet, storagePath(p, id, "/objects"), "", finance).Code)
	s.expectError(s.manage(http.MethodPost, projectPath(p, ""), `{"bucket_name":"more","region":"vno1","package":"10gb"}`, finance), http.StatusForbidden, 60, 10004)
	s.expectError(s.manage(http.MethodPost, storagePath(p, id, "/access-keys"), `{"name":"ci","permission":"read"}`, finance), http.StatusForbidden, 60, 10004)
	s.expectError(s.manage(http.MethodPost, storagePath(p, id, "/objects/upload-url"), `{"key":"a.txt"}`, finance), http.StatusForbidden, 60, 10004)
	s.expectError(s.manage(http.MethodPost, storagePath(p, id, "/objects/delete"), `{"keys":["a.txt"]}`, finance), http.StatusForbidden, 60, 10004)
	s.Equal(http.StatusOK, s.manage(http.MethodPost, storagePath(p, id, "/objects/download-url"), `{"key":"a.txt"}`, finance).Code)

	s.Equal(http.StatusCreated, s.manage(http.MethodPost, projectPath(p, ""), `{"bucket_name":"more","region":"vno1","package":"10gb"}`, developer).Code)
	s.Equal(http.StatusCreated, s.manage(http.MethodPost, storagePath(p, id, "/access-keys"), `{"name":"ci","permission":"read"}`, developer).Code)
}

func (s *storageSuite) TestAccess_AStorageOfAnotherProjectIsNotFound() {
	p, other := s.grantProject(), s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.expectError(s.manage(http.MethodGet, storagePath(other, id, ""), "", other.Owner), http.StatusNotFound, 63, 10001)
}

func (s *storageSuite) TestAccess_ARemovedMemberLosesStorage() {
	p := s.grantProject()
	developer := s.member(p.TeamID, "developer")
	s.Equal(http.StatusOK, s.manage(http.MethodGet, projectPath(p, ""), "", developer).Code)

	s.consume(constants.TeamMemberRemoved, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"removed_at":%q}}`,
		p.TeamID, developer.ID, time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)))
	s.expectError(s.manage(http.MethodGet, projectPath(p, ""), "", developer), http.StatusNotFound, 60, 10001)
}
