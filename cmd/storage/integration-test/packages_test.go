package integrationtest

import (
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
)

// A larger Package applies at once: the engine's limit rises, and billing
// sees the old interval stop and the new one start at the same instant.
func (s *storageSuite) TestPackages_UpgradeIsImmediate() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.clock.Advance(time.Hour)

	upgraded := s.choose(p, id, "250gb")
	s.Require().Equal(http.StatusOK, upgraded.Code, upgraded.Raw)
	s.Equal("250gb", upgraded.obj("package")["code"])
	s.Equal("10.00", upgraded.obj("package")["monthly_price"])
	s.sameInstant(start.Add(time.Hour).Format(time.RFC3339Nano), upgraded.obj("package")["started_at"])

	bucket, _ := s.engine.Bucket("shop-assets")
	s.Equal(int64(250_000_000_000), bucket.SizeLimit)

	stopped := s.outboxPayloads(constants.StoragePackageStopped)
	s.Require().Len(stopped, 1)
	started := s.outboxPayloads(constants.StoragePackageStarted)
	s.Require().Len(started, 2)
	s.Equal(started[0]["started_at"], stopped[0]["started_at"], "the stop names the interval it closes")
	s.Equal(started[1]["started_at"], stopped[0]["stopped_at"], "the new interval starts where the old one stops")
	s.Equal("10.00", started[1]["monthly_price"])
	s.Equal(id, stopped[0]["storage_id"])
}

func (s *storageSuite) TestPackages_DowngradeWaitsForTheNextPeriodAndCanBeUndone() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "250gb").str("id")

	scheduled := s.choose(p, id, "10gb")
	s.Require().Equal(http.StatusOK, scheduled.Code, scheduled.Raw)
	s.Equal("250gb", scheduled.obj("package")["code"], "this Period is already owed")
	s.Equal("10gb", scheduled.obj("scheduled_change")["code"])
	s.sameInstant("2026-11-01T00:00:00Z", scheduled.obj("scheduled_change")["effective_at"])
	bucket, _ := s.engine.Bucket("shop-assets")
	s.Equal(int64(250_000_000_000), bucket.SizeLimit)

	undone := s.choose(p, id, "250gb")
	s.Nil(undone.Body["scheduled_change"])
	s.Len(s.outboxPayloads(constants.StoragePackageStopped), 0)
}

func (s *storageSuite) TestPackages_ADowngradeTheStorageDoesNotFitIsRefused() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "250gb").str("id")
	s.engine.Fill("shop-assets", 30_000_000_000, 12)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))

	s.expectError(s.choose(p, id, "10gb"), http.StatusConflict, 63, 10006)
	s.Equal(http.StatusOK, s.choose(p, id, "50gb").Code)
}

func (s *storageSuite) TestPackages_ChoosingNeedsAnActiveTeam() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.setBilling(p.TeamID, "no_payment_method", time.Now())
	s.expectError(s.choose(p, id, "250gb"), http.StatusPaymentRequired, 61, 10003)
	s.setBilling(p.TeamID, "suspended", time.Now().Add(time.Second))
	s.expectError(s.choose(p, id, "250gb"), http.StatusLocked, 61, 10004)
	s.expectError(s.choose(p, id, "7gb"), http.StatusBadRequest, 62, 10002)
}

// The rollover applies a scheduled change at the boundary instant, not when
// the job runs.
func (s *storageSuite) TestPackages_RolloverAppliesTheScheduleAtTheBoundary() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "250gb").str("id")
	s.Require().Equal(http.StatusOK, s.choose(p, id, "10gb").Code)

	s.clock.Set(time.Date(2026, 11, 1, 0, 7, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.RollPeriods(s.T().Context()))

	r := s.get(p, id)
	s.Equal("10gb", r.obj("package")["code"])
	s.Nil(r.Body["scheduled_change"])
	bucket, _ := s.engine.Bucket("shop-assets")
	s.Equal(int64(10_000_000_000), bucket.SizeLimit)

	stopped := s.outboxPayloads(constants.StoragePackageStopped)
	s.Require().Len(stopped, 1)
	s.sameInstant("2026-11-01T00:00:00Z", stopped[0]["stopped_at"])
	started := s.outboxPayloads(constants.StoragePackageStarted)
	s.Require().Len(started, 2)
	s.sameInstant("2026-11-01T00:00:00Z", started[1]["started_at"])
	s.Equal("10gb", started[1]["package_code"])

	s.Require().NoError(s.jobs.RollPeriods(s.T().Context()))
	s.Len(s.outboxPayloads(constants.StoragePackageStarted), 2, "a second run changes nothing")
}

// A smaller Package keeps the price it was scheduled at, whatever the catalog
// says when the Period turns.
func (s *storageSuite) TestPackages_AScheduledPackageKeepsThePriceItWasChosenAt() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "250gb").str("id")
	s.Require().Equal(http.StatusOK, s.choose(p, id, "10gb").Code)
	s.exec(`UPDATE packages SET monthly_price = 2 WHERE code = '10gb'`)
	defer s.exec(`UPDATE packages SET monthly_price = 1 WHERE code = '10gb'`)

	s.clock.Set(time.Date(2026, 11, 1, 0, 7, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.RollPeriods(s.T().Context()))

	s.Equal("1.00", s.get(p, id).obj("package")["monthly_price"])
}

// A Storage that no longer fits its scheduled Package at the boundary stays
// where it is, and the Team is told.
func (s *storageSuite) TestPackages_ADowngradeTheStorageOutgrewIsSkipped() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "250gb").str("id")
	s.Require().Equal(http.StatusOK, s.choose(p, id, "10gb").Code)
	s.engine.Fill("shop-assets", 20_000_000_000, 5)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))

	s.clock.Set(time.Date(2026, 11, 1, 0, 7, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.RollPeriods(s.T().Context()))

	r := s.get(p, id)
	s.Equal("250gb", r.obj("package")["code"])
	s.Nil(r.Body["scheduled_change"])
	s.Len(s.outboxPayloads(constants.StoragePackageStopped), 0)

	skipped := s.outboxPayloads(constants.StorageDowngradeSkipped)
	s.Require().Len(skipped, 1)
	s.Equal("250gb", skipped[0]["package_code"])
	s.Equal("10gb", skipped[0]["skipped_package_code"])
	s.Equal(float64(10_000_000_000), skipped[0]["skipped_size_bytes"])
	s.Equal(float64(20_000_000_000), skipped[0]["used_bytes"])
	s.Equal("shop-assets", skipped[0]["bucket_name"])
}
