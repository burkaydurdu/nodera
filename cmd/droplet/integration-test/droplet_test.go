package integrationtest

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/application/jobs"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/pkg/apperror"
)

// ── Create → poll → active ────────────────────────────────────────────────────

func (s *dropletSuite) TestCreate_ProvisioningThenActiveWithAddressesAndHistory() {
	u := s.signIn()
	_, projectID := s.grantProject(u)

	created := s.createDroplet(u, projectID, "web-1")

	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.Equal("provisioning", created.str("status"))
	s.Equal("web-1", created.str("hostname"))
	s.Equal("c4-32", created.str("plan_code"))
	s.Equal("vno1", created.str("region_code"))
	s.Equal("ubuntu-24-04", created.str("image_code"))
	s.Equal("0.4920", created.str("hourly_price"), "the catalog price is locked onto the Droplet")
	s.Equal("330.63", created.str("monthly_price"))
	s.Equal(projectID.String(), created.str("project_id"))
	s.Nil(created.Body["ipv4"])
	s.Nil(created.Body["active_at"])
	keys, _ := created.Body["ssh_keys"].([]any)
	s.Require().Len(keys, 1)
	key, _ := keys[0].(map[string]any)
	s.Equal("key-1", key["name"])
	s.Regexp(`^SHA256:`, key["fingerprint"], "the service computes fingerprints itself")
	s.NotContains(created.Raw, "public_key", "key material is never echoed")

	list := s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token)
	s.Require().Len(list.List, 1)
	s.Equal("provisioning", list.List[0]["status"])
	s.Nil(list.List[0]["history"], "history is a detail-only field")

	s.Require().Len(s.fake.Specs, 1, "the Provider was asked once")
	s.Equal("e5-1620v4", s.fake.Specs[0].PlanRef)
	s.Equal(regionLT, s.fake.Specs[0].RegionRef)
	s.Equal("ubuntu_24_04_64bit", s.fake.Specs[0].ImageRef)
	s.Equal("web-1", s.fake.Specs[0].Hostname)
	s.Len(s.fake.Specs[0].SSHKeyRefs, 1)

	s.poll()
	s.Equal("provisioning", s.get("/v1/droplets/"+created.str("id"), u.Token).str("status"), "the Provider has not delivered yet")

	s.fake.Advance()
	s.poll()

	detail := s.get("/v1/droplets/"+created.str("id"), u.Token)
	s.Require().Equal(http.StatusOK, detail.Code, detail.Raw)
	s.Equal("active", detail.str("status"))
	s.NotEmpty(detail.str("ipv4"))
	s.NotEmpty(detail.str("ipv6"))
	s.NotEmpty(detail.str("active_at"))
	s.Nil(detail.Body["deletion_requested_at"])
	s.Equal("0.4920", detail.str("hourly_price"))
	history, _ := detail.Body["history"].([]any)
	s.Require().Len(history, 2)
	first, _ := history[0].(map[string]any)
	second, _ := history[1].(map[string]any)
	s.Nil(first["from"])
	s.Equal("provisioning", first["to"])
	s.Equal("provisioning", second["from"])
	s.Equal("active", second["to"])
}

func (s *dropletSuite) TestCreate_ListsNewestFirst() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.Require().Equal(http.StatusCreated, s.createDroplet(u, projectID, "first").Code)
	s.Require().Equal(http.StatusCreated, s.createDroplet(u, projectID, "second").Code)

	list := s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token)

	s.Require().Len(list.List, 2)
	s.Equal("second", list.List[0]["hostname"])
	s.Equal("first", list.List[1]["hostname"])
}

// ── Live Provider check at creation ──────────────────────────────────────────

func (s *dropletSuite) TestCreate_SoldOutAtTheProviderIsRefusedAndFlagsThePlan() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.SetStock("e5-1620v4", regionLT, 0)

	res := s.createDroplet(u, projectID, "web-1")

	s.Equal(catalog.ErrorPlanSoldOut.HttpStatus, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(catalog.Domain), domain)
	s.Equal(catalog.ErrorPlanSoldOut.Code, code)
	s.Len(s.fake.Specs, 0, "nothing was ordered")
	s.Len(s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List, 0, "nothing was recorded")
	s.Equal(true, s.get("/v1/catalog/plans?region=vno1", u.Token).item("code", "c4-32")["sold_out"], "the Plan is flagged before the next sync")
}

func (s *dropletSuite) TestCreate_AlreadySoldOutPlanIsRefused() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.SetStock("e5-1620v4", regionLT, 0)
	s.sync()

	res := s.createDroplet(u, projectID, "web-1")

	s.Equal(catalog.ErrorPlanSoldOut.HttpStatus, res.Code, res.Raw)
}

func (s *dropletSuite) TestCreate_ProviderPriceUpIsRefusedAndReprices() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.SetPrice("e5-1620v4", regionLT, "0.50")

	res := s.createDroplet(u, projectID, "web-1")

	s.Equal(catalog.ErrorPlanPriceChanged.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(catalog.ErrorPlanPriceChanged.Code, code)
	s.Len(s.fake.Specs, 0)
	s.Equal("0.6000", s.get("/v1/catalog/plans?region=vno1", u.Token).item("code", "c4-32")["hourly_price"], "the catalog now shows the new price")

	again := s.createDroplet(u, projectID, "web-1")
	s.Equal(http.StatusCreated, again.Code, again.Raw)
	s.Equal("0.6000", again.str("hourly_price"), "created at the price the customer saw after refreshing")
}

func (s *dropletSuite) TestCreate_ProviderPriceDownProceedsAtTheCatalogPrice() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.SetPrice("e5-1620v4", regionLT, "0.30")

	res := s.createDroplet(u, projectID, "web-1")

	s.Equal(http.StatusCreated, res.Code, res.Raw)
	s.Equal("0.4920", res.str("hourly_price"), "never more than shown; the lower price is Nodera's gain")
}

// ── Provider failure ─────────────────────────────────────────────────────────

func (s *dropletSuite) TestCreate_ProviderRefusalFailsTheDropletWithASafeReason() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.FailCreate(provider.ErrorRejected.Copy().SetError(fmt.Errorf("cherry: insufficient balance on team 4242")))

	created := s.createDroplet(u, projectID, "web-1")

	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.Equal("provisioning", created.str("status"), "the customer sees the accepted request")

	detail := s.get("/v1/droplets/"+created.str("id"), u.Token)
	s.Equal("failed", detail.str("status"))
	s.Equal(provider.ErrorRejected.Message, detail.str("failure_reason"))
	s.NotContains(detail.Raw, "cherry", "the raw Provider message stays in the log")
	s.NotContains(detail.Raw, "balance")
	history, _ := detail.Body["history"].([]any)
	s.Len(history, 2)
	s.Nil(detail.Body["active_at"], "a failed Droplet never counts as usage")
}

func (s *dropletSuite) TestCreate_UnknownProviderErrorIsGenericToTheCustomer() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.FailCreate(fmt.Errorf("dial tcp: connection refused"))

	created := s.createDroplet(u, projectID, "web-1")

	detail := s.get("/v1/droplets/"+created.str("id"), u.Token)
	s.Equal("failed", detail.str("status"))
	s.Equal(provider.ErrorUnavailable.Message, detail.str("failure_reason"))
}

// ── Power actions ────────────────────────────────────────────────────────────

func (s *dropletSuite) TestActions_PowerOffThenOnThenRebootFromLiveStatuses() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	actions := "/v1/droplets/" + id + "/actions"

	off := s.request(http.MethodPost, actions, `{"type":"power_off"}`, u.Token)
	s.Require().Equal(http.StatusAccepted, off.Code, off.Raw)
	s.Equal("active", off.str("status"), "the status follows the poller, not the request")

	second := s.request(http.MethodPost, actions, `{"type":"reboot"}`, u.Token)
	s.Equal(droplet.ErrorActionNotAllowed.HttpStatus, second.Code, "one action at a time")

	s.fake.Advance()
	s.poll()
	s.Equal("off", s.get("/v1/droplets/"+id, u.Token).str("status"))

	on := s.request(http.MethodPost, actions, `{"type":"power_on"}`, u.Token)
	s.Require().Equal(http.StatusAccepted, on.Code, on.Raw)
	s.fake.Advance()
	s.poll()
	s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"))

	reboot := s.request(http.MethodPost, actions, `{"type":"reboot"}`, u.Token)
	s.Require().Equal(http.StatusAccepted, reboot.Code, reboot.Raw)
	s.fake.Advance()
	s.poll()
	detail := s.get("/v1/droplets/"+id, u.Token)
	s.Equal("active", detail.str("status"))
	history, _ := detail.Body["history"].([]any)
	s.Len(history, 4, "created, active, off, active: a reboot is not a transition")

	after := s.request(http.MethodPost, actions, `{"type":"power_off"}`, u.Token)
	s.Equal(http.StatusAccepted, after.Code, "the pending marker cleared after the reboot")
}

func (s *dropletSuite) TestActions_RefusedWhileProvisioningOrFailed() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	provisioning := s.createDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusCreated, provisioning.Code)

	res := s.request(http.MethodPost, "/v1/droplets/"+provisioning.str("id")+"/actions", `{"type":"reboot"}`, u.Token)
	s.Equal(droplet.ErrorActionNotAllowed.HttpStatus, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(droplet.Domain), domain)
	s.Equal(droplet.ErrorActionNotAllowed.Code, code)

	s.fake.FailCreate(provider.ErrorRejected)
	failed := s.createDroplet(u, projectID, "web-2")
	s.Require().Equal("failed", s.get("/v1/droplets/"+failed.str("id"), u.Token).str("status"))
	res = s.request(http.MethodPost, "/v1/droplets/"+failed.str("id")+"/actions", `{"type":"power_on"}`, u.Token)
	s.Equal(droplet.ErrorActionNotAllowed.HttpStatus, res.Code, res.Raw)
}

// The pending marker is written before the Provider is asked, so a poll that
// lands in between cannot miss it; a refused request leaves no marker behind.
func (s *dropletSuite) TestActions_ProviderFailureLeavesNoPendingMarker() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	actions := "/v1/droplets/" + id + "/actions"
	s.fake.FailAction(provider.ErrorUnavailable.Copy().SetError(fmt.Errorf("cherry: 503")))

	res := s.request(http.MethodPost, actions, `{"type":"power_off"}`, u.Token)

	s.Equal(provider.ErrorUnavailable.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(provider.ErrorUnavailable.Code, code)

	s.fake.FailAction(nil)
	again := s.request(http.MethodPost, actions, `{"type":"power_off"}`, u.Token)
	s.Equal(http.StatusAccepted, again.Code, "no pending action was left behind by the refused one")
}

func (s *dropletSuite) TestActions_UnknownTypeIsRejected() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")

	res := s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"explode"}`, u.Token)
	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
}

// ── Delete ───────────────────────────────────────────────────────────────────

func (s *dropletSuite) TestDelete_DeletingThenDeletedAndHiddenFromTheList() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")

	res := s.request(http.MethodDelete, "/v1/droplets/"+id, "", u.Token)

	s.Require().Equal(http.StatusAccepted, res.Code, res.Raw)
	s.Equal("deleting", res.str("status"))
	s.NotEmpty(res.str("deletion_requested_at"), "the meter stops at the request")

	list := s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token)
	s.Require().Len(list.List, 1)
	s.Equal("deleting", list.List[0]["status"], "still visible while the Provider releases it")

	again := s.request(http.MethodDelete, "/v1/droplets/"+id, "", u.Token)
	s.Equal(droplet.ErrorActionNotAllowed.HttpStatus, again.Code, "a deleting Droplet cannot be acted on again")
	s.Equal(droplet.ErrorActionNotAllowed.HttpStatus, s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"power_on"}`, u.Token).Code)

	s.poll()

	detail := s.get("/v1/droplets/"+id, u.Token)
	s.Equal("deleted", detail.str("status"))
	s.Equal(res.str("deletion_requested_at"), detail.str("deletion_requested_at"), "the request time is not moved by the release")
	s.Len(s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List, 0, "deleted Droplets leave the list")

	var rows int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM droplets WHERE id = $1`, id).Scan(&rows))
	s.Equal(1, rows, "never a hard delete")
}

func (s *dropletSuite) TestDelete_FailedDropletWithoutAServerIsDeletedAtOnce() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.FailCreate(provider.ErrorRejected)
	failed := s.createDroplet(u, projectID, "web-1")
	s.fake.FailCreate(nil)

	res := s.request(http.MethodDelete, "/v1/droplets/"+failed.str("id"), "", u.Token)

	s.Require().Equal(http.StatusAccepted, res.Code, res.Raw)
	s.Equal("deleted", res.str("status"))
	s.Len(s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List, 0)
}

// A customer may abandon a Droplet still provisioning: the server the
// Provider already created is released through the usual deleting path.
func (s *dropletSuite) TestDelete_ProvisioningDropletWithAServerIsReleasedByThePoller() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	created := s.createDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	ref := s.providerRef(created.str("id"))

	res := s.request(http.MethodDelete, "/v1/droplets/"+created.str("id"), "", u.Token)

	s.Require().Equal(http.StatusAccepted, res.Code, res.Raw)
	s.Equal("deleting", res.str("status"))
	server, ok := s.fake.Server(ref)
	s.Require().True(ok)
	s.Equal(providers.StatusDeleted, server.Status, "the Provider was asked to release it")

	s.poll()
	s.Equal("deleted", s.get("/v1/droplets/"+created.str("id"), u.Token).str("status"))
}

// The Provider has not answered the order yet: there is no server to release,
// so the Droplet is deleted at once instead of waiting for a poll.
func (s *dropletSuite) TestDelete_ProvisioningDropletWithoutAServerIsDeletedAtOnce() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	created := s.createDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	_, err := s.pgPool.Exec(s.T().Context(), `UPDATE droplets SET provider_ref = NULL WHERE id = $1`, created.str("id"))
	s.Require().NoError(err)

	res := s.request(http.MethodDelete, "/v1/droplets/"+created.str("id"), "", u.Token)

	s.Require().Equal(http.StatusAccepted, res.Code, res.Raw)
	s.Equal("deleted", res.str("status"))
	s.NotEmpty(res.str("deletion_requested_at"))
}

// The Provider refusing the release is not the end of it: the Droplet stays
// deleting and every poll asks again until the server is gone.
func (s *dropletSuite) TestDelete_ProviderFailureIsRetriedByThePoller() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	ref := s.providerRef(id)
	s.fake.FailDelete(provider.ErrorUnavailable.Copy().SetError(fmt.Errorf("cherry: 503")))

	res := s.request(http.MethodDelete, "/v1/droplets/"+id, "", u.Token)
	s.Require().Equal(http.StatusAccepted, res.Code, res.Raw)
	s.Equal("deleting", res.str("status"), "the meter stops even when the Provider is down")

	s.Error(s.jobs.PollDroplets(s.T().Context()), "the failed retry is reported")
	s.Equal("deleting", s.get("/v1/droplets/"+id, u.Token).str("status"))
	server, _ := s.fake.Server(ref)
	s.Equal(providers.StatusActive, server.Status, "still running at the Provider")

	s.fake.FailDelete(nil)
	s.poll()

	s.Equal("deleted", s.get("/v1/droplets/"+id, u.Token).str("status"))
	server, _ = s.fake.Server(ref)
	s.Equal(providers.StatusDeleted, server.Status)
}

// ── Provisioning timeout ─────────────────────────────────────────────────────

func (s *dropletSuite) TestPoll_ProvisioningTimeoutFailsTheDropletAndReleasesTheServer() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	created := s.createDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	ref := s.providerRef(created.str("id"))
	s.jobs.WithClock(func() time.Time { return time.Now().Add(jobs.ProvisioningTimeout + time.Minute) })
	defer s.jobs.WithClock(time.Now)

	s.poll()

	detail := s.get("/v1/droplets/"+created.str("id"), u.Token)
	s.Equal("failed", detail.str("status"))
	s.Equal(droplet.ErrorProvisioningTimedOut.Message, detail.str("failure_reason"))
	s.NotContains(detail.Raw, "cherry")
	server, ok := s.fake.Server(ref)
	s.Require().True(ok)
	s.Equal(providers.StatusDeleted, server.Status, "Nodera stops paying for a server it never delivered")
}

// ── Validation ───────────────────────────────────────────────────────────────

func (s *dropletSuite) TestCreate_InvalidHostnameIsRejected() {
	u := s.signIn()
	_, projectID := s.grantProject(u)

	for _, hostname := range []string{"-leading", "trailing-", "has space", "under_score", strings.Repeat("a", 64), ""} {
		res := s.createDroplet(u, projectID, hostname)
		s.Equal(http.StatusBadRequest, res.Code, "hostname %q: %s", hostname, res.Raw)
	}
	s.Len(s.fake.Specs, 0)

	upper := s.createDroplet(u, projectID, "Web-Server-01")
	s.Equal(http.StatusCreated, upper.Code, upper.Raw)
	s.Equal("web-server-01", upper.str("hostname"), "hostnames are lower-cased, not refused")
}

func (s *dropletSuite) TestCreate_NeedsAtLeastOneSSHKey() {
	u := s.signIn()
	_, projectID := s.grantProject(u)

	res := s.request(http.MethodPost, "/v1/projects/"+projectID.String()+"/droplets",
		`{"plan":"c4-32","region":"vno1","image":"ubuntu-24-04","hostname":"web-1","ssh_keys":[]}`, u.Token)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
	s.Contains(res.Raw, `"field":"ssh_keys"`, "the error names the field the client sent")
}

func (s *dropletSuite) TestCreate_MoreThanTenKeysIsRejected() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	keys := make([]string, 11)
	for i := range keys {
		keys[i] = testKey
	}

	res := s.request(http.MethodPost, "/v1/projects/"+projectID.String()+"/droplets", createBody("web-1", keys...), u.Token)

	s.Equal(http.StatusBadRequest, res.Code, res.Raw)
}

func (s *dropletSuite) TestCreate_GarbageSSHKeyIsRejected() {
	u := s.signIn()
	_, projectID := s.grantProject(u)

	res := s.request(http.MethodPost, "/v1/projects/"+projectID.String()+"/droplets", createBody("web-1", "not a key at all"), u.Token)

	s.Equal(droplet.ErrorInvalidSSHKey.HttpStatus, res.Code, res.Raw)
	_, code := res.errorCode()
	s.Equal(droplet.ErrorInvalidSSHKey.Code, code)
}

func (s *dropletSuite) TestCreate_TwoDistinctKeysAreBothRemembered() {
	u := s.signIn()
	_, projectID := s.grantProject(u)

	res := s.request(http.MethodPost, "/v1/projects/"+projectID.String()+"/droplets", createBody("web-1", testKey, testKey2), u.Token)

	s.Require().Equal(http.StatusCreated, res.Code, res.Raw)
	keys, _ := res.Body["ssh_keys"].([]any)
	s.Len(keys, 2)
	s.Len(s.fake.Specs[0].SSHKeyRefs, 2)
}

func (s *dropletSuite) TestCreate_UnknownPlanRegionOrImageIsNotFound() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	path := "/v1/projects/" + projectID.String() + "/droplets"

	plan := s.request(http.MethodPost, path, `{"plan":"c99-1","region":"vno1","image":"ubuntu-24-04","hostname":"a","ssh_keys":[{"name":"k","public_key":"`+testKey+`"}]}`, u.Token)
	s.Equal(catalog.ErrorPlanNotFound.HttpStatus, plan.Code, plan.Raw)

	region := s.request(http.MethodPost, path, `{"plan":"c4-32","region":"tyo1","image":"ubuntu-24-04","hostname":"a","ssh_keys":[{"name":"k","public_key":"`+testKey+`"}]}`, u.Token)
	s.Equal(catalog.ErrorPlanNotFound.HttpStatus, region.Code, "c4-32 is not sold in tyo1")

	image := s.request(http.MethodPost, path, `{"plan":"c4-32","region":"vno1","image":"windows-11","hostname":"a","ssh_keys":[{"name":"k","public_key":"`+testKey+`"}]}`, u.Token)
	s.Equal(catalog.ErrorImageNotFound.HttpStatus, image.Code, image.Raw)
}

// ── Rate limits ──────────────────────────────────────────────────────────────

func (s *dropletSuite) TestCreate_RateLimitedAfterTenInTenMinutes() {
	u := s.signIn()
	_, projectID := s.grantProject(u)

	for i := 1; i <= 10; i++ {
		res := s.createDroplet(u, projectID, fmt.Sprintf("web-%d", i))
		s.Require().Equal(http.StatusCreated, res.Code, res.Raw)
	}

	res := s.createDroplet(u, projectID, "web-11")

	s.Equal(apperror.ErrorRateLimited.HttpStatus, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(apperror.Domain), domain)
	s.Equal(apperror.ErrorRateLimited.Code, code)
	s.Equal("600", res.Hdr.Get("Retry-After"))

	other := s.signIn()
	_, otherProject := s.grantProject(other)
	s.Equal(http.StatusCreated, s.createDroplet(other, otherProject, "web-1").Code, "the budget is per User")
}

func (s *dropletSuite) TestActions_RateLimitedAfterThirtyInTenMinutes() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	actions := "/v1/droplets/" + id + "/actions"

	for i := 1; i <= 30; i++ {
		res := s.request(http.MethodPost, actions, `{"type":"reboot"}`, u.Token)
		s.Require().Equal(http.StatusAccepted, res.Code, res.Raw)
		s.fake.Advance()
		s.poll()
	}

	res := s.request(http.MethodPost, actions, `{"type":"reboot"}`, u.Token)

	s.Equal(apperror.ErrorRateLimited.HttpStatus, res.Code, res.Raw)
	s.Equal("600", res.Hdr.Get("Retry-After"))
}
