package integrationtest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/cmd/droplet/app"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/billing"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/logger"
)

// ── Payment method required ──────────────────────────────────────────────────

func (s *dropletSuite) TestBilling_CreateWithoutAPaymentMethodIsRefused() {
	u := s.signIn()
	_, projectID := s.grantProjectWithoutBilling(u)

	res := s.createDroplet(u, projectID, "web-1")

	s.Equal(droplet.ErrorPaymentMethodRequired.HttpStatus, res.Code, res.Raw)
	domain, code := res.errorCode()
	s.Equal(int(droplet.Domain), domain)
	s.Equal(droplet.ErrorPaymentMethodRequired.Code, code)
	s.Len(s.fake.Specs, 0, "nothing was ordered")
	s.Len(s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List, 0, "nothing was recorded")
}

func (s *dropletSuite) TestBilling_NoPaymentMethodAgainAfterTheLastCardIsRemoved() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	s.setBilling(teamID, "no_payment_method", time.Now())

	res := s.createDroplet(u, projectID, "web-1")

	s.Equal(droplet.ErrorPaymentMethodRequired.HttpStatus, res.Code, res.Raw)
}

func (s *dropletSuite) TestBilling_AccessIsCheckedBeforeBillingStatus() {
	owner := s.signIn()
	_, projectID := s.grantProjectWithoutBilling(owner)
	stranger := s.signIn()

	res := s.createDroplet(stranger, projectID, "sneaky")

	s.Equal(http.StatusNotFound, res.Code, "a stranger learns nothing about the Team's billing")
}

// ── The meter ────────────────────────────────────────────────────────────────

func (s *dropletSuite) TestBilling_StartedOnceWhenTheDropletFirstBecomesActive() {
	u := s.signIn()
	teamID, projectID := s.grantProjectWithoutBilling(u)
	s.Equal(droplet.ErrorPaymentMethodRequired.HttpStatus, s.createDroplet(u, projectID, "web-1").Code)

	s.setBilling(teamID, "active", time.Now())
	created := s.createDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStarted), "nothing is billed while provisioning")

	s.poll()
	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStarted), "the Provider has not delivered yet")

	s.fake.Advance()
	s.poll()

	detail := s.get("/v1/droplets/"+created.str("id"), u.Token)
	s.Require().Equal("active", detail.str("status"), detail.Raw)
	started := s.outboxPayload(constants.DropletBillingStarted, created.str("id"))
	s.Equal(teamID.String(), started["team_id"])
	s.Equal(projectID.String(), started["project_id"])
	s.Equal("web-1", started["hostname"])
	s.Equal("c4-32", started["plan_code"])
	s.Equal("0.4920", started["hourly_price"], "the Locked price, as a 4-decimal string")
	s.isRFC3339UTC(started["started_at"])
	s.sameInstant(detail.str("active_at"), started["started_at"])
	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStopped))

	s.poll()
	s.poll()
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStarted), "a redelivered poll announces nothing")

	off := s.request(http.MethodPost, "/v1/droplets/"+created.str("id")+"/actions", `{"type":"power_off"}`, u.Token)
	s.Require().Equal(http.StatusAccepted, off.Code, off.Raw)
	s.fake.Advance()
	s.poll()
	s.Equal("off", s.get("/v1/droplets/"+created.str("id"), u.Token).str("status"))
	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStopped), "a powered-off server is still rented")
}

func (s *dropletSuite) TestBilling_StoppedAtTheDeletionRequest() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")

	res := s.request(http.MethodDelete, "/v1/droplets/"+id, "", u.Token)
	s.Require().Equal(http.StatusAccepted, res.Code, res.Raw)

	stopped := s.outboxPayload(constants.DropletBillingStopped, id)
	s.Equal(teamID.String(), stopped["team_id"])
	s.isRFC3339UTC(stopped["stopped_at"])
	s.sameInstant(res.str("deletion_requested_at"), stopped["stopped_at"])

	s.poll()
	s.Equal("deleted", s.get("/v1/droplets/"+id, u.Token).str("status"))
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStopped), "the release does not stop the meter again")
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStarted))
}

func (s *dropletSuite) TestBilling_StoppedWhenARunningDropletFails() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	reboot := s.request(http.MethodPost, "/v1/droplets/"+id+"/actions", `{"type":"reboot"}`, u.Token)
	s.Require().Equal(http.StatusAccepted, reboot.Code, reboot.Raw)
	s.fake.FailGet(provider.ErrorServerNotFound)

	s.poll()

	detail := s.get("/v1/droplets/"+id, u.Token)
	s.Require().Equal("failed", detail.str("status"), detail.Raw)
	stopped := s.outboxPayload(constants.DropletBillingStopped, id)
	s.isRFC3339UTC(stopped["stopped_at"])
	history, _ := detail.Body["history"].([]any)
	last, _ := history[len(history)-1].(map[string]any)
	s.Equal("failed", last["to"])
	s.sameInstant(last["at"], stopped["stopped_at"])

	s.fake.FailGet(nil)
	deleted := s.request(http.MethodDelete, "/v1/droplets/"+id, "", u.Token)
	s.Require().Equal(http.StatusAccepted, deleted.Code, deleted.Raw)
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStopped), "the meter stopped at the failure, not again at the deletion")
}

func (s *dropletSuite) TestBilling_NothingForADropletThatNeverRan() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	s.fake.FailCreate(provider.ErrorRejected)
	failed := s.createDroplet(u, projectID, "web-1")
	s.Require().Equal("failed", s.get("/v1/droplets/"+failed.str("id"), u.Token).str("status"))
	s.fake.FailCreate(nil)

	abandoned := s.createDroplet(u, projectID, "web-2")
	s.Require().Equal(http.StatusCreated, abandoned.Code, abandoned.Raw)
	s.Require().Equal(http.StatusAccepted, s.request(http.MethodDelete, "/v1/droplets/"+abandoned.str("id"), "", u.Token).Code)
	s.poll()

	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStarted))
	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStopped), "no usage began, so there is nothing to stop")
}

// ── Suspension ───────────────────────────────────────────────────────────────

func (s *dropletSuite) TestBilling_SuspensionRefusesCreateAndPowerOnAndPowersOffRunningDroplets() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	running1 := s.activeDroplet(u, projectID, "web-1")
	running2 := s.activeDroplet(u, projectID, "web-2")
	parked := s.activeDroplet(u, projectID, "web-3")
	s.Require().Equal(http.StatusAccepted, s.request(http.MethodPost, "/v1/droplets/"+parked+"/actions", `{"type":"power_off"}`, u.Token).Code)
	s.fake.Advance()
	s.poll()
	s.Require().Equal("off", s.get("/v1/droplets/"+parked, u.Token).str("status"), "the customer parked this one before the suspension")
	ref1, ref2, refParked := s.providerRef(running1), s.providerRef(running2), s.providerRef(parked)

	s.setBilling(teamID, "suspended", time.Now())

	create := s.createDroplet(u, projectID, "web-4")
	s.Equal(droplet.ErrorTeamSuspended.HttpStatus, create.Code, create.Raw)
	domain, code := create.errorCode()
	s.Equal(int(droplet.Domain), domain)
	s.Equal(droplet.ErrorTeamSuspended.Code, code)

	powerOn := s.request(http.MethodPost, "/v1/droplets/"+parked+"/actions", `{"type":"power_on"}`, u.Token)
	s.Equal(droplet.ErrorTeamSuspended.HttpStatus, powerOn.Code, powerOn.Raw)
	_, code = powerOn.errorCode()
	s.Equal(droplet.ErrorTeamSuspended.Code, code)

	s.fake.Advance()
	s.poll()

	for _, ref := range []string{ref1, ref2, refParked} {
		server, ok := s.fake.Server(ref)
		s.Require().True(ok)
		s.Equal(providers.StatusOff, server.Status, "the Provider was asked to power off %s", ref)
	}
	for _, id := range []string{running1, running2} {
		detail := s.get("/v1/droplets/"+id, u.Token)
		s.Equal("off", detail.str("status"), detail.Raw)
		history, _ := detail.Body["history"].([]any)
		last, _ := history[len(history)-1].(map[string]any)
		s.Equal("off", last["to"])
		s.Equal("suspended", last["reason"], "the customer learns why their server went down")
	}
	parkedDetail := s.get("/v1/droplets/"+parked, u.Token)
	parkedHistory, _ := parkedDetail.Body["history"].([]any)
	s.Len(parkedHistory, 3, "created, active, off: the parked Droplet was not touched")
	s.Equal(int64(0), s.outboxCount(constants.DropletBillingStopped), "a suspended Droplet is still rented")

	s.setBilling(teamID, "active", time.Now())

	for _, id := range []string{running1, running2} {
		s.Equal("off", s.get("/v1/droplets/"+id, u.Token).str("status"), "the status follows the poller")
	}
	s.fake.Advance()
	s.poll()
	for _, id := range []string{running1, running2} {
		s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"), "powered on again after the recovery")
	}
	s.Equal("off", s.get("/v1/droplets/"+parked, u.Token).str("status"), "what the customer powered off stays off")
	server, _ := s.fake.Server(refParked)
	s.Equal(providers.StatusOff, server.Status)

	s.Equal(http.StatusCreated, s.createDroplet(u, projectID, "web-4").Code)
	s.Equal(http.StatusAccepted, s.request(http.MethodPost, "/v1/droplets/"+parked+"/actions", `{"type":"power_on"}`, u.Token).Code)
}

func (s *dropletSuite) TestBilling_SuspensionRedeliveredIsHarmless() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	suspendedAt := time.Now()

	s.setBilling(teamID, "suspended", suspendedAt)
	s.setBilling(teamID, "suspended", suspendedAt)
	s.fake.Advance()
	s.poll()
	s.setBilling(teamID, "suspended", suspendedAt)

	detail := s.get("/v1/droplets/"+id, u.Token)
	s.Equal("off", detail.str("status"))
	history, _ := detail.Body["history"].([]any)
	s.Len(history, 3, "created, active, off: powered off once")
	s.poll()
	s.Equal("off", s.get("/v1/droplets/"+id, u.Token).str("status"))
}

func (s *dropletSuite) TestBilling_AnOlderStatusEventIsIgnored() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")

	s.setBilling(teamID, "suspended", time.Now().Add(-time.Hour))

	s.Equal(http.StatusCreated, s.createDroplet(u, projectID, "web-2").Code, "the newer active status stands")
	s.poll()
	s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"), "nothing was powered off")
}

func (s *dropletSuite) TestBilling_UnknownStatusIsMalformed() {
	teamID, _ := s.grantProject(s.signIn())

	err := s.consumeErr(constants.TeamBillingStatusChanged, fmt.Sprintf(`{"payload":{"team_id":%q,"status":"frozen","changed_at":%q}}`, teamID, time.Now().UTC().Format(time.RFC3339)))

	var rerr rerror.Error
	s.Require().True(errors.As(err, &rerr), "%v", err)
	s.Equal(billing.Domain, rerr.Domain)
	s.Equal(billing.ErrorMalformedEvent.Code, rerr.Code)
}

// ── Deletion request ─────────────────────────────────────────────────────────

func (s *dropletSuite) TestBilling_DeletionRequestReleasesEveryDropletOfTheTeam() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	running := s.activeDroplet(u, projectID, "web-1")
	parked := s.activeDroplet(u, projectID, "web-2")
	s.Require().Equal(http.StatusAccepted, s.request(http.MethodPost, "/v1/droplets/"+parked+"/actions", `{"type":"power_off"}`, u.Token).Code)
	s.fake.Advance()
	s.poll()
	other := s.signIn()
	_, otherProject := s.grantProject(other)
	untouched := s.activeDroplet(other, otherProject, "web-9")
	// Created last: every Advance above would have delivered it.
	ordering := s.createDroplet(u, projectID, "web-3")
	s.Require().Equal(http.StatusCreated, ordering.Code, ordering.Raw)

	s.requestTeamDeletion(teamID)

	for _, id := range []string{running, parked, ordering.str("id")} {
		detail := s.get("/v1/droplets/"+id, u.Token)
		s.Equal("deleting", detail.str("status"), detail.Raw)
		s.NotEmpty(detail.str("deletion_requested_at"))
		history, _ := detail.Body["history"].([]any)
		last, _ := history[len(history)-1].(map[string]any)
		s.Equal("unpaid", last["reason"])
	}
	s.Equal("active", s.get("/v1/droplets/"+untouched, other.Token).str("status"), "another Team's Droplet is not touched")
	s.Equal(int64(2), s.outboxCount(constants.DropletBillingStopped), "the two that ran stop; the one still ordering never started")
	for _, id := range []string{running, parked} {
		s.sameInstant(s.get("/v1/droplets/"+id, u.Token).str("deletion_requested_at"), s.outboxPayload(constants.DropletBillingStopped, id)["stopped_at"])
	}

	s.requestTeamDeletion(teamID)
	s.Equal(int64(2), s.outboxCount(constants.DropletBillingStopped), "a redelivered request changes nothing")

	s.poll()
	for _, id := range []string{running, parked, ordering.str("id")} {
		s.Equal("deleted", s.get("/v1/droplets/"+id, u.Token).str("status"))
	}
	s.Len(s.get("/v1/projects/"+projectID.String()+"/droplets", u.Token).List, 0)
	s.requestTeamDeletion(teamID)
}

// ── Replay ───────────────────────────────────────────────────────────────────

func (s *dropletSuite) TestBilling_ReplayEmitsOneStartedPerDropletThatRan() {
	u := s.signIn()
	_, projectID := s.grantProject(u)
	running := s.activeDroplet(u, projectID, "web-1")
	parked := s.activeDroplet(u, projectID, "web-2")
	s.Require().Equal(http.StatusAccepted, s.request(http.MethodPost, "/v1/droplets/"+parked+"/actions", `{"type":"power_off"}`, u.Token).Code)
	s.fake.Advance()
	s.poll()
	deleted := s.activeDroplet(u, projectID, "web-3")
	s.Require().Equal(http.StatusAccepted, s.request(http.MethodDelete, "/v1/droplets/"+deleted, "", u.Token).Code)
	s.poll()
	ordering := s.createDroplet(u, projectID, "web-4")
	s.Require().Equal(http.StatusCreated, ordering.Code, ordering.Raw)
	s.fake.FailCreate(provider.ErrorRejected)
	s.createDroplet(u, projectID, "web-5")
	s.fake.FailCreate(nil)
	s.clearOutbox()

	summary, err := app.ReplayBilling(context.Background(), logger.Noop(), s.pgPool, s.registry, s.outbox)

	s.Require().NoError(err)
	s.Equal(3, summary.Started)
	s.Equal(1, summary.Stopped)
	s.Equal(int64(3), s.outboxCount(constants.DropletBillingStarted))
	s.Equal(int64(1), s.outboxCount(constants.DropletBillingStopped))
	for _, id := range []string{running, parked, deleted} {
		started := s.outboxPayload(constants.DropletBillingStarted, id)
		s.Equal("0.4920", started["hourly_price"])
		s.sameInstant(s.get("/v1/droplets/"+id, u.Token).str("active_at"), started["started_at"])
	}
	stopped := s.outboxPayload(constants.DropletBillingStopped, deleted)
	s.sameInstant(s.get("/v1/droplets/"+deleted, u.Token).str("deletion_requested_at"), stopped["stopped_at"])

	again, err := app.ReplayBilling(context.Background(), logger.Noop(), s.pgPool, s.registry, s.outbox)
	s.Require().NoError(err)
	s.Equal(summary, again, "a replay answers the same set every time")

	s.Require().Equal(http.StatusAccepted, s.request(http.MethodDelete, "/v1/droplets/"+running, "", u.Token).Code)
	s.Equal(int64(6), s.outboxCount(constants.DropletBillingStarted), "the delete after a replay does not announce Started again")
	s.Equal(int64(3), s.outboxCount(constants.DropletBillingStopped))
}

// A suspension cannot act on a Droplet still being ordered or busy with an
// action of its own; the poller powers it off once it settles, and marks it
// so the recovery powers it back on.
func (s *dropletSuite) TestBilling_SuspensionCatchesDropletsThatSettleLater() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	rebooting := s.activeDroplet(u, projectID, "web-1")
	s.Require().Equal(http.StatusAccepted, s.request(http.MethodPost, "/v1/droplets/"+rebooting+"/actions", `{"type":"reboot"}`, u.Token).Code)
	ordering := s.createDroplet(u, projectID, "web-2")
	s.Require().Equal(http.StatusCreated, ordering.Code, ordering.Raw)
	refRebooting, refOrdering := s.providerRef(rebooting), s.providerRef(ordering.str("id"))

	s.setBilling(teamID, "suspended", time.Now())

	server, _ := s.fake.Server(refOrdering)
	s.Equal(providers.StatusProvisioning, server.Status, "nothing to power off yet")

	s.fake.Advance()
	s.poll()

	s.outboxPayload(constants.DropletBillingStarted, ordering.str("id"))
	s.fake.Advance()
	s.poll()

	for _, id := range []string{rebooting, ordering.str("id")} {
		detail := s.get("/v1/droplets/"+id, u.Token)
		s.Equal("off", detail.str("status"), detail.Raw)
		history, _ := detail.Body["history"].([]any)
		last, _ := history[len(history)-1].(map[string]any)
		s.Equal("suspended", last["reason"])
	}
	for _, ref := range []string{refRebooting, refOrdering} {
		server, _ := s.fake.Server(ref)
		s.Equal(providers.StatusOff, server.Status)
	}

	s.setBilling(teamID, "active", time.Now())
	s.fake.Advance()
	s.poll()

	for _, id := range []string{rebooting, ordering.str("id")} {
		s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"), "the recovery powers on what the poller powered off")
	}
}

// The recovery may arrive while the suspension's power-off is still in
// flight; the poller powers the Droplet back on once it lands.
func (s *dropletSuite) TestBilling_RecoveryDuringTheSuspensionPowerOff() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")

	s.setBilling(teamID, "suspended", time.Now())
	s.setBilling(teamID, "active", time.Now())
	s.fake.Advance()
	s.poll()

	s.Equal("off", s.get("/v1/droplets/"+id, u.Token).str("status"), "the power-off landed")
	s.fake.Advance()
	s.poll()
	s.Equal("active", s.get("/v1/droplets/"+id, u.Token).str("status"), "and the poller powered it back on")
}

func (s *dropletSuite) TestBilling_SuspendedTeamMayNotRebootButMayPowerOff() {
	u := s.signIn()
	teamID, projectID := s.grantProject(u)
	id := s.activeDroplet(u, projectID, "web-1")
	actions := "/v1/droplets/" + id + "/actions"
	s.setBilling(teamID, "suspended", time.Now())
	s.fake.Advance()
	s.poll()
	s.Require().Equal("off", s.get("/v1/droplets/"+id, u.Token).str("status"))

	reboot := s.request(http.MethodPost, actions, `{"type":"reboot"}`, u.Token)

	s.Equal(droplet.ErrorTeamSuspended.HttpStatus, reboot.Code, reboot.Raw)
	_, code := reboot.errorCode()
	s.Equal(droplet.ErrorTeamSuspended.Code, code)

	off := s.request(http.MethodPost, actions, `{"type":"power_off"}`, u.Token)
	s.Equal(http.StatusAccepted, off.Code, "powering off is never refused for billing: %s", off.Raw)
	s.fake.Advance()
	s.poll()

	s.setBilling(teamID, "active", time.Now())
	s.fake.Advance()
	s.poll()
	s.Equal("off", s.get("/v1/droplets/"+id, u.Token).str("status"), "the customer's own power-off outlives the suspension")
}
