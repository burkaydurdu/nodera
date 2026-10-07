package integrationtest

import (
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
	"github.com/piggystats/nodera/contexts/storage/engine"
)

func (s *storageSuite) TestMeasure_WhatTheEngineCountsIsShown() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.Fill("shop-assets", 1_234_567, 3)

	s.Require().NoError(s.jobs.Measure(s.T().Context()))

	r := s.get(p, id)
	s.Equal(float64(1_234_567), r.num("used_bytes"))
	s.Equal(float64(3), r.num("object_count"))
	s.sameInstant(start.Format(time.RFC3339Nano), r.Body["measured_at"])
	s.Equal("active", r.str("state"))
	s.Len(s.outboxPayloads(constants.StorageNearlyFull), 0)
}

// The marks are told once per crossing and armed again once the Storage is
// back under them, so a second filling is told again.
func (s *storageSuite) TestMeasure_NearlyFullAndFullAreToldOncePerFilling() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.engine.Fill("shop-assets", 8_100_000_000, 10)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))
	s.Require().NoError(s.jobs.Measure(s.T().Context()))
	nearly := s.outboxPayloads(constants.StorageNearlyFull)
	s.Require().Len(nearly, 1)
	s.Equal(map[string]any{
		"team_id": p.TeamID.String(), "project_id": p.ProjectID.String(), "project_name": "Shop", "storage_id": id,
		"bucket_name": "shop-assets", "package_code": "10gb", "size_bytes": float64(10_000_000_000), "used_bytes": float64(8_100_000_000),
		"at": nearly[0]["at"],
	}, nearly[0])

	s.engine.Fill("shop-assets", 10_000_000_000, 12)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))
	s.Len(s.outboxPayloads(constants.StorageFull), 1)
	s.Equal("full", s.get(p, id).str("state"))

	s.engine.Fill("shop-assets", 1_000_000_000, 1)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))
	s.Equal("active", s.get(p, id).str("state"))

	s.engine.Fill("shop-assets", 9_000_000_000, 9)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))
	s.Len(s.outboxPayloads(constants.StorageNearlyFull), 2, "a second filling is told again")
	s.Len(s.outboxPayloads(constants.StorageFull), 1)
}

// A Storage that jumps straight to full is told once, that it is full; a
// larger Package arms the marks again.
func (s *storageSuite) TestMeasure_AnUpgradeArmsTheMarksAgain() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.Fill("shop-assets", 10_500_000_000, 12)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))
	s.Len(s.outboxPayloads(constants.StorageNearlyFull), 0)
	s.Len(s.outboxPayloads(constants.StorageFull), 1)

	s.Require().Equal(http.StatusOK, s.choose(p, id, "50gb").Code)
	s.Equal("active", s.get(p, id).str("state"))

	s.engine.Fill("shop-assets", 41_000_000_000, 40)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))
	s.Len(s.outboxPayloads(constants.StorageNearlyFull), 1)
}

func (s *storageSuite) TestMeasure_TrafficIsKeptPerDay() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.AddTraffic("shop-assets", engine.Traffic{Hour: start.Truncate(time.Hour), BytesSent: 1000, BytesReceived: 200, Requests: 7})
	s.engine.AddTraffic("shop-assets", engine.Traffic{Hour: start.Truncate(time.Hour).Add(time.Hour), BytesSent: 50, BytesReceived: 0, Requests: 1})
	s.engine.Fill("shop-assets", 5_000_000, 2)
	s.Require().NoError(s.jobs.Measure(s.T().Context()))

	s.clock.Advance(90 * time.Minute)
	s.Require().NoError(s.jobs.Traffic(s.T().Context()))
	// The hour just ended was still being written: read again, it replaces
	// what was kept and never doubles it.
	s.engine.AddTraffic("shop-assets", engine.Traffic{Hour: start.Truncate(time.Hour), BytesSent: 1500, BytesReceived: 200, Requests: 9})
	s.Require().NoError(s.jobs.Traffic(s.T().Context()))

	r := s.manage(http.MethodGet, storagePath(p, id, "/usage"), "", p.Owner)
	s.Require().Equal(http.StatusOK, r.Code, r.Raw)
	days, _ := r.Body["days"].([]any)
	s.Require().Len(days, 1)
	day := days[0].(map[string]any)
	s.Equal("2026-10-12", day["day"])
	s.Equal(float64(5_000_000), day["used_bytes"])
	s.Equal(float64(2), day["object_count"])
	s.Equal(float64(1550), day["bytes_sent"])
	s.Equal(float64(200), day["bytes_received"])
	s.Equal(float64(10), day["requests"])
}
