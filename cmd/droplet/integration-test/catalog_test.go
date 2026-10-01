package integrationtest

import (
	"net/http"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
)

func (s *dropletSuite) TestCatalog_RequiresASession() {
	s.Equal(http.StatusUnauthorized, s.get("/v1/catalog/regions", "").Code)
	s.Equal(http.StatusUnauthorized, s.get("/v1/catalog/plans?region=vno1", "").Code)
	s.Equal(http.StatusUnauthorized, s.get("/v1/catalog/images", "").Code)
}

func (s *dropletSuite) TestCatalog_RegionsListTheSevenWithPlanCounts() {
	u := s.signIn()

	res := s.get("/v1/catalog/regions", u.Token)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Require().Len(res.List, 7)
	vno := res.item("code", "vno1")
	s.Require().NotNil(vno)
	s.Equal("Šiauliai", vno["city"])
	s.Equal("LT", vno["country"])
	s.EqualValues(3, vno["plan_count"])
	s.EqualValues(0, res.item("code", "tyo1")["plan_count"], "a Region without Plans is still listed")
	s.NotContains(res.Raw, "Cherry", "provider names never reach the customer")
}

func (s *dropletSuite) TestCatalog_PlanPriceIsOfferPlusCommission() {
	u := s.signIn()

	res := s.get("/v1/catalog/plans?region=vno1", u.Token)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	plan := res.item("code", "c4-32")
	s.Require().NotNil(plan, res.Raw)
	s.Equal("0.4920", plan["hourly_price"], "0.41 + 2000 bps, rounded up to 4 decimals")
	s.Equal("330.63", plan["monthly_price"], "0.4920 × 672 rounded up to cents")
	s.Equal(false, plan["sold_out"])
	s.Equal("vno1", plan["region_code"])
	s.EqualValues(4, plan["cpu_count"])
	s.EqualValues(32, plan["ram_gb"])
	s.ElementsMatch([]any{"debian-12", "ubuntu-22-04", "ubuntu-24-04"}, plan["images"])
}

func (s *dropletSuite) TestCatalog_PlanOverrideCommissionWins() {
	u := s.signIn()

	res := s.get("/v1/catalog/plans?region=fra1", u.Token)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	plan := res.item("code", "c8-64")
	s.Require().NotNil(plan, res.Raw)
	s.Equal("0.3657", plan["hourly_price"], "0.318 + 1500 bps override instead of the 2000 default")
	s.Equal("245.76", plan["monthly_price"])
}

func (s *dropletSuite) TestCatalog_PlansNeedARegion() {
	u := s.signIn()

	s.Equal(http.StatusBadRequest, s.get("/v1/catalog/plans", u.Token).Code)

	unknown := s.get("/v1/catalog/plans?region=mars1", u.Token)
	s.Equal(catalog.ErrorRegionNotFound.HttpStatus, unknown.Code, unknown.Raw)
	domain, code := unknown.errorCode()
	s.Equal(int(catalog.Domain), domain)
	s.Equal(catalog.ErrorRegionNotFound.Code, code)
}

func (s *dropletSuite) TestCatalog_ImagesListTheLaunchSet() {
	u := s.signIn()

	res := s.get("/v1/catalog/images", u.Token)

	s.Equal(http.StatusOK, res.Code, res.Raw)
	s.Require().Len(res.List, 3)
	ubuntu := res.item("code", "ubuntu-24-04")
	s.Require().NotNil(ubuntu)
	s.Equal("ubuntu", ubuntu["family"])
	s.Equal("24.04", ubuntu["version"])
}

func (s *dropletSuite) TestCatalog_SyncMarksAPlanSoldOutWhenStockIsZero() {
	u := s.signIn()
	s.fake.SetStock("e5-1620v4", regionLT, 0)
	s.sync()

	res := s.get("/v1/catalog/plans?region=vno1", u.Token)

	plan := res.item("code", "c4-32")
	s.Require().NotNil(plan, "a sold-out Plan stays listed")
	s.Equal(true, plan["sold_out"])
	s.Equal("0.4920", plan["hourly_price"], "the price is unchanged")
	s.Equal(false, res.item("code", "c8-64")["sold_out"], "other Plans are untouched")

	s.fake.SetStock("e5-1620v4", regionLT, 5)
	s.sync()
	s.Equal(false, s.get("/v1/catalog/plans?region=vno1", u.Token).item("code", "c4-32")["sold_out"], "stock back means for sale again")
}

func (s *dropletSuite) TestCatalog_SyncMarksAPlanSoldOutWhenItsOfferVanishes() {
	u := s.signIn()
	s.fake.RemovePlan("e3-1240v3")
	s.sync()

	res := s.get("/v1/catalog/plans?region=vno1", u.Token)

	plan := res.item("code", "c4-16")
	s.Require().NotNil(plan, "a vanished Offer never deletes the Plan")
	s.Equal(true, plan["sold_out"])
}

func (s *dropletSuite) TestCatalog_SyncFollowsAProviderPriceChange() {
	u := s.signIn()
	s.fake.SetPrice("e5-1620v4", regionLT, "0.50")
	s.sync()

	plan := s.get("/v1/catalog/plans?region=vno1", u.Token).item("code", "c4-32")

	s.Equal("0.6000", plan["hourly_price"], "the Commission rides on the new price")
	s.Equal("403.20", plan["monthly_price"])
}

// A Provider that lists nothing hourly-priced is a broken answer, not an empty
// catalog: expiring every Offer on it would sell the whole catalog out.
func (s *dropletSuite) TestCatalog_SyncWithNoHourlyPlansLeavesTheCatalogAlone() {
	u := s.signIn()
	for _, ref := range []string{"e3-1240v3", "e5-1620v4", "amd-ryzen-7700x", "amd-epyc-9124p"} {
		s.fake.RemovePlan(ref)
	}
	s.sync()

	res := s.get("/v1/catalog/plans?region=vno1", u.Token)

	s.Require().NotNil(res.item("code", "c4-32"), res.Raw)
	s.Equal(false, res.item("code", "c4-32")["sold_out"])
	s.Equal(false, res.item("code", "c4-16")["sold_out"])
	s.Equal("0.4920", res.item("code", "c4-32")["hourly_price"])
}
