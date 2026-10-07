package integrationtest

import (
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
)

// Deleting ends the Package and the keys at once; the files go with the purge
// job, and only then is the Storage gone.
func (s *storageSuite) TestDeletion_EndsThePackageAtOnceAndTheFilesWithThePurge() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.createKey(p, id, "ci", "read", p.Owner)
	s.clock.Advance(2 * time.Hour)

	deleted := s.deleteStorage(p, id, "shop-assets")
	s.Require().Equal(http.StatusOK, deleted.Code, deleted.Raw)
	s.Equal("deleting", deleted.str("status"))
	s.Equal("deleting", deleted.str("state"))

	stopped := s.outboxPayloads(constants.StoragePackageStopped)
	s.Require().Len(stopped, 1)
	s.sameInstant(start.Add(2*time.Hour).Format(time.RFC3339Nano), stopped[0]["stopped_at"])
	bucket, held := s.engine.Bucket("shop-assets")
	s.Require().True(held, "the files wait for the purge")
	s.True(bucket.Stopped, "nothing reads or writes a Storage that is going")
	s.Len(s.manage(http.MethodGet, storagePath(p, id, "/access-keys"), "", p.Owner).List, 0)
	s.expectError(s.createKey(p, id, "late", "read", p.Owner), http.StatusConflict, 63, 10009)
	s.expectError(s.choose(p, id, "50gb"), http.StatusConflict, 63, 10009)

	s.Require().NoError(s.jobs.Purge(s.T().Context()))
	_, held = s.engine.Bucket("shop-assets")
	s.False(held)
	s.expectError(s.manage(http.MethodGet, storagePath(p, id, ""), "", p.Owner), http.StatusNotFound, 63, 10001)
	s.Len(s.manage(http.MethodGet, projectPath(p, ""), "", p.Owner).List, 0)
}

func (s *storageSuite) TestDeletion_RepeatsTheBucketName() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.expectError(s.deleteStorage(p, id, "shop-asset"), http.StatusConflict, 63, 10007)
	s.Equal(http.StatusBadRequest, s.manage(http.MethodDelete, storagePath(p, id, ""), "", p.Owner).Code)
	s.Equal("active", s.get(p, id).str("status"))
}

// A Paused Storage and a suspended Team's may be deleted: it is how a Team
// removes what was complained about, and it owes nothing more for it.
func (s *storageSuite) TestDeletion_IsAllowedWhilePausedOrSuspended() {
	p := s.grantProject()
	paused := s.opened(p, "paused-one", "10gb").str("id")
	suspended := s.opened(p, "the-other", "10gb").str("id")
	_, err := s.handlers.Operations.Pause(s.T().Context(), mustUUID(paused), "phishing")
	s.Require().NoError(err)
	s.setBilling(p.TeamID, "suspended", time.Now())

	s.Equal(http.StatusOK, s.deleteStorage(p, paused, "paused-one").Code)
	s.Equal(http.StatusOK, s.deleteStorage(p, suspended, "the-other").Code)
	s.Len(s.outboxPayloads(constants.StoragePackageStopped), 2)
}

// A purge the engine refused is tried again; the Storage stays "deleting"
// until the files are really gone.
func (s *storageSuite) TestDeletion_AFailedPurgeIsRetried() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.Require().Equal(http.StatusOK, s.deleteStorage(p, id, "shop-assets").Code)

	s.engine.FailWith(errUnreachable)
	s.Require().NoError(s.jobs.Purge(s.T().Context()))
	_, held := s.engine.Bucket("shop-assets")
	s.True(held)

	s.engine.FailWith(nil)
	s.Require().NoError(s.jobs.Purge(s.T().Context()))
	_, held = s.engine.Bucket("shop-assets")
	s.False(held)
}
