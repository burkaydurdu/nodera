package integrationtest

import (
	"net/http"
	"time"
)

func (s *kubernetesSuite) TestCatalog_VersionsNewestFirst() {
	u := s.signIn()

	r := s.manage(http.MethodGet, "/v1/kubernetes/versions", "", u)

	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	s.Require().Len(r.List, 2)
	s.Equal("1.37.0", r.List[0]["version"])
	s.Equal("1.36.4", r.List[1]["version"])
}

func (s *kubernetesSuite) TestCatalog_OnlyTheFleetRegionIsSold() {
	u := s.signIn()

	r := s.manage(http.MethodGet, "/v1/kubernetes/regions", "", u)

	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	s.Require().Len(r.List, 1, "the droplet catalog sells two Regions; one has a control plane fleet")
	s.Equal(region, r.List[0]["code"])
	s.Equal("LT", r.List[0]["country"])
}

func (s *kubernetesSuite) TestCatalog_PlansLargeEnoughForANode() {
	u := s.signIn()

	r := s.manage(http.MethodGet, "/v1/kubernetes/regions/"+region+"/plans", "", u)

	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	codes := make([]string, 0)
	for _, p := range r.List {
		codes = append(codes, p["code"].(string))
	}
	s.Equal([]string{"c2-16", "c4-32", "c8-64"}, codes, "cheapest first; the one-cpu plan is no node")
	s.Equal(true, r.List[0]["sold_out"], "a sold-out plan stays listed and flagged")
	s.Equal("0.2766", r.List[1]["hourly_price"])
	s.Equal("185.88", r.List[1]["monthly_price"])
}

func (s *kubernetesSuite) TestCatalog_ARegionWithoutAFleetIsNotSold() {
	u := s.signIn()

	s.expectError(s.manage(http.MethodGet, "/v1/kubernetes/regions/ams1/plans", "", u), http.StatusBadRequest, 72, 10005)
	s.expectError(s.manage(http.MethodGet, "/v1/kubernetes/regions/nowhere/plans", "", u), http.StatusNotFound, 72, 10001)
}

// The read model follows the newest announcement: an older copy arriving late
// changes nothing, and a Plan the catalog stopped selling disappears.
func (s *kubernetesSuite) TestCatalog_FollowsTheNewestAnnouncement() {
	u := s.signIn()

	s.announcePlanAt("c4-32", region, 4, 32, "0.9999", "671.93", true, true, s.clock.Now().Add(-time.Hour))
	r := s.manage(http.MethodGet, "/v1/kubernetes/regions/"+region+"/plans", "", u)
	s.Equal("0.2766", r.List[1]["hourly_price"], "the older announcement changed nothing")

	s.announcePlanAt("c4-32", region, 4, 32, "0.3000", "201.60", false, false, s.clock.Now().Add(time.Minute))
	r = s.manage(http.MethodGet, "/v1/kubernetes/regions/"+region+"/plans", "", u)
	s.Require().Len(r.List, 2, "a disabled Plan is not sold")
}
