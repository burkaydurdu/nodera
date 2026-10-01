package providers_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/contexts/droplet/providers/cherry"
	"github.com/piggystats/nodera/contexts/droplet/providers/fake"
	"github.com/piggystats/nodera/logger"
)

// The contract is one table of read-only expectations every adapter meets.
// It runs against the fake always and against Cherry Servers when the smoke
// toggle and a token are set; it never creates anything supplier-side.
type contractSuite struct {
	suite.Suite
	provider providers.Provider
	// mappedImage is an image ref the adapter is expected to list for the
	// first plan.
	mappedImage string
}

func (s *contractSuite) TestListRegions_NonEmptyWithRefs() {
	regions, err := s.provider.ListRegions(context.Background())
	s.Require().NoError(err)
	s.Require().NotEmpty(regions)
	for _, r := range regions {
		s.NotEmpty(r.Ref)
	}
}

func (s *contractSuite) TestListPlans_CarryHourlyPricesAndStock() {
	plans, err := s.provider.ListPlans(context.Background())
	s.Require().NoError(err)
	s.Require().NotEmpty(plans)
	for _, p := range plans {
		s.NotEmpty(p.Ref)
		for _, r := range p.Regions {
			s.NotEmpty(r.RegionRef, p.Ref)
			s.True(r.HourlyPrice.IsPositive(), "%s in %s has no hourly price", p.Ref, r.RegionRef)
			s.GreaterOrEqual(r.Stock, 0)
		}
	}
}

func (s *contractSuite) TestListImages_IncludeTheMappedImage() {
	plans, err := s.provider.ListPlans(context.Background())
	s.Require().NoError(err)
	s.Require().NotEmpty(plans)

	images, err := s.provider.ListImages(context.Background(), plans[0].Ref)
	s.Require().NoError(err)
	refs := make([]string, 0, len(images))
	for _, i := range images {
		refs = append(refs, i.Ref)
	}
	s.Contains(refs, s.mappedImage)
}

func TestFakeContract(t *testing.T) {
	f := fake.New("fake")
	f.AddRegion("LT-Siauliai", "Lithuania, Šiauliai", "LT")
	f.AddImage("ubuntu_24_04_64bit", "Ubuntu 24.04 64bit")
	f.SetOffer("e5-1620v4", "LT-Siauliai", "0.41", 3)

	suite.Run(t, &contractSuite{provider: f, mappedImage: "ubuntu_24_04_64bit"})
}

func TestCherryContract_Smoke(t *testing.T) {
	if os.Getenv("NODERA_DROPLET_SMOKE") != "true" {
		t.Skip("set NODERA_DROPLET_SMOKE=true and NODERA_DROPLET_PROVIDERS_CHERRY_TOKEN to run the read-only smoke test")
	}
	token := os.Getenv("NODERA_DROPLET_PROVIDERS_CHERRY_TOKEN")
	if token == "" {
		t.Skip("NODERA_DROPLET_PROVIDERS_CHERRY_TOKEN is not set")
	}
	projectID, _ := strconv.Atoi(os.Getenv("NODERA_DROPLET_PROVIDERS_CHERRY_PROJECT_ID"))
	if projectID == 0 {
		// Read-only calls need no project; any positive id satisfies the constructor.
		projectID = 1
	}

	c, err := cherry.New(cherry.Config{Token: token, ProjectID: projectID, Endpoint: os.Getenv("NODERA_DROPLET_PROVIDERS_CHERRY_ENDPOINT")}, logger.Noop())
	require.NoError(t, err)

	suite.Run(t, &contractSuite{provider: c, mappedImage: "ubuntu_24_04_64bit"})
}
