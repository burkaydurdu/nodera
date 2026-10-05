package integrationtest

import (
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/mail/async/constants"
)

func (s *mailSuite) TestPackages_CatalogIsTheLaunchTable() {
	p := s.grantProject()

	r := s.manage(http.MethodGet, "/v1/mail/packages", "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	s.Require().Len(r.List, 5)
	s.Equal(map[string]any{"code": "5k", "monthly_emails": float64(5000), "monthly_price": "5.00"}, r.List[0])
	s.Equal(map[string]any{"code": "100k", "monthly_emails": float64(100000), "monthly_price": "49.00"}, r.List[4])
}

func (s *mailSuite) TestPackages_OverviewWithoutAPackage() {
	p := s.grantProject()

	o := s.overview(p)
	s.Nil(o.Body["package"])
	s.Equal("no_package", o.str("sending_state"))
	s.Equal("active", o.str("billing_status"))
	s.Equal(float64(200), o.obj("daily_limit")["limit"])
}

func (s *mailSuite) TestPackages_ChoosingNeedsAPaymentMethodAndAnActiveTeam() {
	p := s.grantProjectWithoutBilling()
	s.expectError(s.choose(p, "10k"), http.StatusPaymentRequired, 51, 10003)

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.expectError(s.choose(p, "10k"), http.StatusLocked, 51, 10004)

	s.expectError(s.choose(s.grantProject(), "7k"), http.StatusBadRequest, 53, 10001)
}

// A cancellation spends nothing, so it is the one choice a Team that may not
// spend can still make.
func (s *mailSuite) TestPackages_ASuspendedTeamCanOnlyCancel() {
	p, _ := s.ready()
	s.setBilling(p.TeamID, "suspended", time.Now())

	s.expectError(s.choose(p, "5k"), http.StatusLocked, 51, 10004)
	s.Require().Equal(http.StatusOK, s.choose(p, "none").Code)
	s.expectError(s.choose(p, "10k"), http.StatusLocked, 51, 10004)
}

func (s *mailSuite) TestPackages_ATeamWithoutAPaymentMethodCannotUndoACancellation() {
	p, _ := s.ready()
	s.Require().Equal(http.StatusOK, s.choose(p, "none").Code)
	s.setBilling(p.TeamID, "no_payment_method", time.Now())

	s.expectError(s.choose(p, "10k"), http.StatusPaymentRequired, 51, 10003)
	s.Equal("none", s.overview(p).obj("scheduled_change")["code"])
}

func (s *mailSuite) TestPackages_FirstChoiceStartsAtOnce() {
	p := s.grantProject()

	chosen := s.choose(p, "10k")
	s.Require().Equal(http.StatusOK, chosen.Code, chosen.Raw)
	s.Equal("10k", chosen.obj("package")["code"])
	s.Equal("9.00", chosen.obj("package")["monthly_price"])
	s.Equal(float64(10000), chosen.obj("period")["allowance"])
	s.Equal(float64(0), chosen.obj("period")["used"])
	s.sameInstant("2026-11-01T00:00:00Z", chosen.obj("period")["end"])
	s.Equal("active", chosen.str("sending_state"))

	started := s.outboxPayloads(constants.MailPackageStarted)
	s.Require().Len(started, 1)
	s.Equal(map[string]any{
		"project_id": p.ProjectID.String(), "team_id": p.TeamID.String(), "project_name": "Shop", "package_code": "10k",
		"monthly_emails": float64(10000), "monthly_price": "9.00", "started_at": started[0]["started_at"],
	}, started[0])
	s.sameInstant(start.Format(time.RFC3339Nano), started[0]["started_at"])
}

// An upgrade applies at once and keeps what was sent counted; billing sees
// the old interval stop and the new one start at the same instant.
func (s *mailSuite) TestPackages_UpgradeIsImmediateAndCarriesTheUsage() {
	p, key := s.ready()
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "a@example.net", "b@example.net").Code)

	s.clock.Advance(time.Hour)
	upgraded := s.choose(p, "50k")
	s.Require().Equal(http.StatusOK, upgraded.Code, upgraded.Raw)
	s.Equal("50k", upgraded.obj("package")["code"])
	s.Equal(float64(50000), upgraded.obj("period")["allowance"])
	s.Equal(float64(2), upgraded.obj("period")["used"])

	stopped := s.outboxPayloads(constants.MailPackageStopped)
	s.Require().Len(stopped, 1)
	started := s.outboxPayloads(constants.MailPackageStarted)
	s.Require().Len(started, 2)
	s.Equal(started[0]["started_at"], stopped[0]["started_at"], "the stop names the interval it closes")
	s.Equal(started[1]["started_at"], stopped[0]["stopped_at"], "the new interval starts where the old one stops")
	s.Equal("29.00", started[1]["monthly_price"])
}

func (s *mailSuite) TestPackages_DowngradeAndCancellationWaitForTheNextPeriod() {
	p, _ := s.ready()

	scheduled := s.choose(p, "5k")
	s.Require().Equal(http.StatusOK, scheduled.Code, scheduled.Raw)
	s.Equal("10k", scheduled.obj("package")["code"], "this Period is already owed")
	s.Equal("5k", scheduled.obj("scheduled_change")["code"])
	s.sameInstant("2026-11-01T00:00:00Z", scheduled.obj("scheduled_change")["effective_at"])

	undone := s.choose(p, "10k")
	s.Nil(undone.Body["scheduled_change"])

	cancelled := s.choose(p, "none")
	s.Equal("none", cancelled.obj("scheduled_change")["code"])
	s.Len(s.outboxPayloads(constants.MailPackageStopped), 0)
}

// The rollover applies a scheduled change at the boundary instant, not when
// the job runs, and renews the allowance.
func (s *mailSuite) TestPackages_RolloverAppliesTheScheduleAtTheBoundary() {
	p, key := s.ready()
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "a@example.net").Code)
	s.Require().Equal(http.StatusOK, s.choose(p, "5k").Code)

	s.clock.Set(time.Date(2026, 11, 1, 0, 7, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.RollPeriods(s.T().Context()))

	o := s.overview(p)
	s.Equal("5k", o.obj("package")["code"])
	s.Nil(o.Body["scheduled_change"])
	s.Equal(float64(5000), o.obj("period")["allowance"])
	s.Equal(float64(0), o.obj("period")["used"])

	stopped := s.outboxPayloads(constants.MailPackageStopped)
	s.Require().Len(stopped, 1)
	s.sameInstant("2026-11-01T00:00:00Z", stopped[0]["stopped_at"])
	started := s.outboxPayloads(constants.MailPackageStarted)
	s.Require().Len(started, 2)
	s.sameInstant("2026-11-01T00:00:00Z", started[1]["started_at"])
	s.Equal("5k", started[1]["package_code"])

	s.Require().NoError(s.jobs.RollPeriods(s.T().Context()))
	s.Len(s.outboxPayloads(constants.MailPackageStarted), 2, "a second run changes nothing")
}

// A smaller Package's price is the one it was chosen at, like any Package's,
// even when the catalog changes before the Period turns.
func (s *mailSuite) TestPackages_AScheduledPackageKeepsThePriceItWasChosenAt() {
	p, _ := s.ready()
	s.Require().Equal(http.StatusOK, s.choose(p, "5k").Code)
	s.exec(`UPDATE packages SET monthly_price = 6 WHERE code = '5k'`)
	defer s.exec(`UPDATE packages SET monthly_price = 5 WHERE code = '5k'`)

	s.clock.Set(time.Date(2026, 11, 1, 0, 7, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.RollPeriods(s.T().Context()))

	s.Equal("5.00", s.overview(p).obj("package")["monthly_price"])
	started := s.outboxPayloads(constants.MailPackageStarted)
	s.Require().Len(started, 2)
	s.Equal("5.00", started[1]["monthly_price"])
}

func (s *mailSuite) TestPackages_CancellationEndsMailAtTheBoundary() {
	p, key := s.ready()
	s.Require().Equal(http.StatusOK, s.choose(p, "none").Code)

	s.clock.Set(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC))
	s.expectError(s.sendTo(key, "no-reply@example.com", "a@example.net"), http.StatusPaymentRequired, 53, 10002)

	o := s.overview(p)
	s.Nil(o.Body["package"])
	stopped := s.outboxPayloads(constants.MailPackageStopped)
	s.Require().Len(stopped, 1)
	s.sameInstant("2026-11-01T00:00:00Z", stopped[0]["stopped_at"])
}
