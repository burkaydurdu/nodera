package integrationtest

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/access"
)

func (s *dropletSuite) TestAccess_UnknownProjectIsNotFound() {
	u := s.signIn()
	unknown := uuid.New()

	list := s.get("/v1/projects/"+unknown.String()+"/droplets", u.Token)
	s.Equal(access.ErrorProjectNotFound.HttpStatus, list.Code, list.Raw)
	domain, code := list.errorCode()
	s.Equal(int(access.Domain), domain)
	s.Equal(access.ErrorProjectNotFound.Code, code)

	create := s.createDroplet(u, unknown, "web-1")
	s.Equal(access.ErrorProjectNotFound.HttpStatus, create.Code, create.Raw)
}

func (s *dropletSuite) TestAccess_AnotherTeamsProjectIsNotFound() {
	owner := s.signIn()
	_, projectID := s.grantProject(owner)
	stranger := s.signIn()
	s.grantProject(stranger)

	list := s.get("/v1/projects/"+projectID.String()+"/droplets", stranger.Token)
	s.Equal(access.ErrorProjectNotFound.HttpStatus, list.Code, "a 404, never a 403: Project ids are not enumerable")

	create := s.createDroplet(stranger, projectID, "sneaky")
	s.Equal(access.ErrorProjectNotFound.HttpStatus, create.Code, create.Raw)
}

func (s *dropletSuite) TestAccess_ConsumedEventsMakeTheProjectKnown() {
	u := s.signIn()
	projectID := uuid.New()
	before := s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token)
	s.Equal(http.StatusNotFound, before.Code)

	_, known := s.grantProject(u)

	after := s.get("/v1/projects/"+known.String()+"/droplets", u.Token)
	s.Equal(http.StatusOK, after.Code, after.Raw)
	s.Len(after.List, 0)
}

func (s *dropletSuite) TestAccess_EventsAreIdempotent() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	s.consume("ProjectCreated", `{"payload":{"project_id":"`+projectID.String()+`","team_id":"`+teamID.String()+`","created_at":"2026-10-01T00:00:00Z"}}`)
	s.consume("TeamMemberAdded", `{"payload":{"team_id":"`+teamID.String()+`","user_id":"`+u.ID.String()+`","role":"owner","added_at":"2026-10-01T00:00:00Z"}}`)

	res := s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token)
	s.Equal(http.StatusOK, res.Code, res.Raw)
}

func (s *dropletSuite) TestAccess_AStrangerCannotSeeADroplet() {
	owner := s.signIn()
	_, projectID := s.grantProject(owner)
	id := s.activeDroplet(owner, projectID, "mine")
	stranger := s.signIn()

	s.Equal(http.StatusNotFound, s.get("/v1/droplets/"+id, stranger.Token).Code)
	s.Equal(http.StatusNotFound, s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"power_off"}`, stranger.Token).Code)
	s.Equal(http.StatusNotFound, s.request(http.MethodDelete, "/v1/droplets/"+id, "", stranger.Token).Code)
	s.Equal("active", s.get("/v1/droplets/"+id, owner.Token).str("status"), "nothing happened to it")
}
