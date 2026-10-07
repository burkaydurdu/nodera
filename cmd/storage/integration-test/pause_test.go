package integrationtest

import (
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
)

// An operator's Pause stops the Storage within the request; the Team is told
// with the reason, and only an operator lifts it.
func (s *storageSuite) TestPause_StopsTheStorageAndTellsTheTeam() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.clock.Advance(time.Minute)

	paused, err := s.handlers.Operations.Pause(s.T().Context(), mustUUID(id), "abuse_complaint")
	s.Require().NoError(err)
	s.True(paused)

	r := s.get(p, id)
	s.Equal("paused", r.str("state"))
	s.Equal("abuse_complaint", r.obj("pause")["reason"])
	s.sameInstant(s.clock.Now().Format(time.RFC3339Nano), r.obj("pause")["paused_at"])
	bucket, _ := s.engine.Bucket("shop-assets")
	s.True(bucket.Stopped)
	s.expectError(s.createKey(p, id, "ci", "read", p.Owner), http.StatusLocked, 63, 10008)
	s.expectError(s.manage(http.MethodPost, storagePath(p, id, "/objects/download-url"), `{"key":"a.txt"}`, p.Owner), http.StatusLocked, 63, 10008)

	told := s.outboxPayloads(constants.StoragePaused)
	s.Require().Len(told, 1)
	s.Equal("abuse_complaint", told[0]["reason"])
	s.Equal("shop-assets", told[0]["bucket_name"])
	s.Equal(id, told[0]["storage_id"])

	again, err := s.handlers.Operations.Pause(s.T().Context(), mustUUID(id), "malware")
	s.Require().NoError(err)
	s.False(again, "already paused")
	s.Equal("abuse_complaint", s.get(p, id).obj("pause")["reason"])

	lifted, err := s.handlers.Operations.Unpause(s.T().Context(), mustUUID(id))
	s.Require().NoError(err)
	s.True(lifted)
	s.Equal("active", s.get(p, id).str("state"))
	bucket, _ = s.engine.Bucket("shop-assets")
	s.False(bucket.Stopped)
}

func (s *storageSuite) TestPause_UnknownReasonAndStorage() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	_, err := s.handlers.Operations.Pause(s.T().Context(), mustUUID(id), "spam")
	s.Error(err)
	s.Nil(s.get(p, id).Body["pause"])
}

// A Storage that is both Paused and suspended comes back only when neither
// holds.
func (s *storageSuite) TestPause_PayingDoesNotUndoAPause() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	_, err := s.handlers.Operations.Pause(s.T().Context(), mustUUID(id), "legal")
	s.Require().NoError(err)
	s.setBilling(p.TeamID, "suspended", time.Now())
	s.Equal("suspended", s.get(p, id).str("state"))

	s.setBilling(p.TeamID, "active", time.Now().Add(time.Second))
	s.Equal("paused", s.get(p, id).str("state"))
	bucket, _ := s.engine.Bucket("shop-assets")
	s.True(bucket.Stopped)

	_, err = s.handlers.Operations.Unpause(s.T().Context(), mustUUID(id))
	s.Require().NoError(err)
	s.Equal("active", s.get(p, id).str("state"))
	bucket, _ = s.engine.Bucket("shop-assets")
	s.False(bucket.Stopped)
}
