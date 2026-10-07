package integrationtest

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
)

var errUnreachable = errors.New("connection refused")

func mustUUID(raw string) uuid.UUID {
	return uuid.MustParse(raw)
}

// A suspended Team's Storages stop being read and written while their files
// stay; paying brings them back without anybody doing anything.
func (s *storageSuite) TestBilling_SuspensionStopsTheStoragesAndPaymentResumesThem() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.Put("shop-assets", "a.txt", 10, start)

	s.setBilling(p.TeamID, "suspended", time.Now())
	r := s.get(p, id)
	s.Equal("suspended", r.str("state"))
	s.Equal("suspended", r.str("billing_status"))
	bucket, _ := s.engine.Bucket("shop-assets")
	s.True(bucket.Stopped)
	s.Len(bucket.Objects, 1, "the files stay")
	s.expectError(s.createKey(p, id, "ci", "read", p.Owner), http.StatusLocked, 61, 10004)
	s.expectError(s.manage(http.MethodGet, storagePath(p, id, "/objects"), "", p.Owner), http.StatusLocked, 61, 10004)
	s.Contains(s.realtimeTypes(), constants.RealtimeStorageChanged)

	s.setBilling(p.TeamID, "active", time.Now().Add(time.Second))
	s.Equal("active", s.get(p, id).str("state"))
	bucket, _ = s.engine.Bucket("shop-assets")
	s.False(bucket.Stopped)
	s.Equal(http.StatusCreated, s.createKey(p, id, "ci", "read", p.Owner).Code)
}

func (s *storageSuite) TestBilling_AnOlderStatusChangesNothing() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.setBilling(p.TeamID, "suspended", time.Now().Add(-time.Hour))
	s.Equal("active", s.get(p, id).str("state"))
}

// Removing the card is not an outage: existing Storages keep working, only
// what would be charged anew is refused.
func (s *storageSuite) TestBilling_NoPaymentMethodKeepsExistingStoragesWorking() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.setBilling(p.TeamID, "no_payment_method", time.Now())
	s.Equal("active", s.get(p, id).str("state"))
	s.Equal(http.StatusCreated, s.createKey(p, id, "ci", "read", p.Owner).Code)
	s.expectError(s.open(p, "another", "10gb"), http.StatusPaymentRequired, 61, 10003)
	s.expectError(s.choose(p, id, "50gb"), http.StatusPaymentRequired, 61, 10003)
}

// Day 21 of Dunning deletes the Team's Storages with its Droplets: each
// Package ends at the event's time and the files go with the purge.
func (s *storageSuite) TestBilling_DeletionDayDeletesTheStorages() {
	p := s.grantProject()
	a := s.opened(p, "images", "10gb").str("id")
	s.opened(p, "backups", "50gb")
	s.setBilling(p.TeamID, "suspended", time.Now())
	s.clock.Advance(time.Hour)

	s.consume(constants.TeamDropletsDeletionRequested, fmt.Sprintf(`{"payload":{"team_id":%q,"requested_at":%q}}`,
		p.TeamID, s.clock.Now().Format(time.RFC3339Nano)))

	stopped := s.outboxPayloads(constants.StoragePackageStopped)
	s.Require().Len(stopped, 2)
	for _, event := range stopped {
		s.sameInstant(s.clock.Now().Format(time.RFC3339Nano), event["stopped_at"])
	}
	s.Equal("deleting", s.get(p, a).str("status"))

	s.Require().NoError(s.jobs.Purge(s.T().Context()))
	s.Len(s.manage(http.MethodGet, projectPath(p, ""), "", p.Owner).List, 0)
	_, held := s.engine.Bucket("images")
	s.False(held)
}

// An engine that was down when the Team was suspended catches up through the
// reconcile job; the database was right all along.
func (s *storageSuite) TestBilling_AnUnreachableEngineCatchesUpThroughReconcile() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.FailWith(errUnreachable)

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.Equal("suspended", s.get(p, id).str("state"))
	bucket, _ := s.engine.Bucket("shop-assets")
	s.False(bucket.Stopped, "the engine could not be told")

	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	bucket, _ = s.engine.Bucket("shop-assets")
	s.False(bucket.Stopped, "still down")

	s.engine.FailWith(nil)
	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	bucket, _ = s.engine.Bucket("shop-assets")
	s.True(bucket.Stopped)

	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	calls := s.engine.Calls()
	s.Equal("Stop shop-assets", calls[len(calls)-1], "a Storage in sync is left alone")
}

// A larger Package the engine did not get is applied by the reconcile job.
func (s *storageSuite) TestBilling_AnUpgradeTheEngineMissedIsReconciled() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.FailWith(errUnreachable)

	s.Require().Equal(http.StatusOK, s.choose(p, id, "50gb").Code)
	bucket, _ := s.engine.Bucket("shop-assets")
	s.Equal(int64(10_000_000_000), bucket.SizeLimit)

	s.engine.FailWith(nil)
	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	bucket, _ = s.engine.Bucket("shop-assets")
	s.Equal(int64(50_000_000_000), bucket.SizeLimit)
}

func (s *storageSuite) TestBilling_MalformedEventsAreRefused() {
	s.Error(s.consumeErr(constants.TeamBillingStatusChanged, `{"payload":{"team_id":"00000000-0000-0000-0000-000000000000","status":"active","changed_at":"2026-10-12T10:00:00Z"}}`))
	s.Error(s.consumeErr(constants.TeamBillingStatusChanged, `{"payload":{"team_id":"0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d","status":"rich","changed_at":"2026-10-12T10:00:00Z"}}`))
	s.Error(s.consumeErr(constants.ProjectCreated, `not json`))
}

// A suspension that arrives while a Storage is still being opened reaches it
// too: the reconcile job stops it as soon as it is open.
func (s *storageSuite) TestBilling_ASuspensionReachesAStorageStillOpening() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.exec(`UPDATE storages SET status = 'opening', package_started_at = NULL WHERE id = $1`, id)

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.exec(`UPDATE storages SET status = 'active', package_started_at = $2 WHERE id = $1`, id, start)

	s.Equal("suspended", s.get(p, id).str("state"))
	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	bucket, _ := s.engine.Bucket("shop-assets")
	s.True(bucket.Stopped)
}
