package integrationtest

import (
	"errors"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
)

func (s *storageSuite) TestCatalog_RegionsAndPackagesAreTheLaunchTables() {
	p := s.grantProject()

	regions := s.manage(http.MethodGet, "/v1/storage/regions", "", p.Owner)
	s.Require().Equal(http.StatusOK, regions.Code, regions.Raw)
	s.Equal([]map[string]any{{"code": "vno1", "city": "Šiauliai", "country": "LT"}}, regions.List)

	packages := s.manage(http.MethodGet, "/v1/storage/packages", "", p.Owner)
	s.Require().Equal(http.StatusOK, packages.Code, packages.Raw)
	s.Require().Len(packages.List, 4)
	s.Equal(map[string]any{"code": "10gb", "size_bytes": float64(10_000_000_000), "monthly_price": "1.00"}, packages.List[0])
	s.Equal(map[string]any{"code": "1tb", "size_bytes": float64(1_000_000_000_000), "monthly_price": "35.00"}, packages.List[3])
}

// Opening answers a usable Storage: the engine holds the bucket at the
// Package's size, and billing sees the Package start at that instant.
func (s *storageSuite) TestStorages_OpeningMakesTheBucketAndStartsThePackage() {
	p := s.grantProject()

	r := s.open(p, "shop-assets", "50gb")
	s.Require().Equal(http.StatusCreated, r.Code, r.Raw)
	s.Equal("shop-assets", r.str("bucket_name"))
	s.Equal("vno1", r.str("region"))
	s.Equal("active", r.str("status"))
	s.Equal("active", r.str("state"))
	s.Equal(testEndpoint, r.str("endpoint"))
	s.Equal("https://shop-assets.vno1.storage.test", r.str("url"))
	s.Equal("50gb", r.obj("package")["code"])
	s.Equal("3.00", r.obj("package")["monthly_price"])
	s.Equal(float64(50_000_000_000), r.obj("package")["size_bytes"])
	s.sameInstant(start.Format(time.RFC3339Nano), r.obj("package")["started_at"])
	s.Nil(r.Body["scheduled_change"])
	s.Equal("active", r.str("billing_status"))

	bucket, ok := s.engine.Bucket("shop-assets")
	s.Require().True(ok, "the engine holds the bucket")
	s.Equal(int64(50_000_000_000), bucket.SizeLimit)
	s.False(bucket.Stopped)

	started := s.outboxPayloads(constants.StoragePackageStarted)
	s.Require().Len(started, 1)
	s.Equal(map[string]any{
		"storage_id": r.str("id"), "team_id": p.TeamID.String(), "project_id": p.ProjectID.String(), "project_name": "Shop",
		"bucket_name": "shop-assets", "region": "vno1", "package_code": "50gb", "size_bytes": float64(50_000_000_000),
		"monthly_price": "3.00", "started_at": started[0]["started_at"],
	}, started[0])
	s.sameInstant(start.Format(time.RFC3339Nano), started[0]["started_at"])
	s.Equal([]string{constants.RealtimeStorageChanged}, s.realtimeTypes())

	list := s.manage(http.MethodGet, projectPath(p, ""), "", p.Owner)
	s.Require().Equal(http.StatusOK, list.Code, list.Raw)
	s.Require().Len(list.List, 1)
	s.Equal(r.str("id"), list.List[0]["id"])
}

func (s *storageSuite) TestStorages_BucketNameRules() {
	p := s.grantProject()

	for _, name := range []string{"ab", "Shop", "shop_assets", "-shop", "shop-", "shop.assets", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz01"} {
		s.expectError(s.open(p, name, "10gb"), http.StatusBadRequest, 63, 10002)
	}
	for _, name := range []string{"admin", "www", "nodera-files", "xn--abc"} {
		s.expectError(s.open(p, name, "10gb"), http.StatusBadRequest, 63, 10003)
	}
	s.Equal(http.StatusCreated, s.open(p, "a1b", "10gb").Code)
	s.Equal(http.StatusCreated, s.open(p, "my-shop-2026", "10gb").Code)
}

// A Bucket name is taken in its Region whichever Team holds it, and free
// again only once the Storage is fully deleted.
func (s *storageSuite) TestStorages_ABucketNameIsTakenAcrossTeams() {
	p, other := s.grantProject(), s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.expectError(s.open(other, "shop-assets", "10gb"), http.StatusConflict, 63, 10004)

	s.Require().Equal(http.StatusOK, s.deleteStorage(p, id, "shop-assets").Code)
	s.expectError(s.open(other, "shop-assets", "10gb"), http.StatusConflict, 63, 10004)

	s.Require().NoError(s.jobs.Purge(s.T().Context()))
	s.Equal(http.StatusCreated, s.open(other, "shop-assets", "10gb").Code)
}

func (s *storageSuite) TestStorages_UnknownRegionAndPackage() {
	p := s.grantProject()

	s.expectError(s.open(p, "shop-assets", "7gb"), http.StatusBadRequest, 62, 10002)
	r := s.manage(http.MethodPost, projectPath(p, ""), `{"bucket_name":"shop-assets","region":"fra1","package":"10gb"}`, p.Owner)
	s.expectError(r, http.StatusBadRequest, 62, 10001)
}

func (s *storageSuite) TestStorages_OpeningNeedsAPaymentMethodAndAnActiveTeam() {
	p := s.grantProjectWithoutBilling()
	s.expectError(s.open(p, "shop-assets", "10gb"), http.StatusPaymentRequired, 61, 10003)

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.expectError(s.open(p, "shop-assets", "10gb"), http.StatusLocked, 61, 10004)

	_, ok := s.engine.Bucket("shop-assets")
	s.False(ok, "nothing reached the engine")
}

// An engine that cannot be reached opens nothing. It may have made the bucket
// before failing, so the name stays reserved until the engine has been asked
// to remove it: at once when it answers, by the reconcile job when it does not.
func (s *storageSuite) TestStorages_AnUnreachableEngineOpensNothing() {
	p := s.grantProject()
	s.engine.FailWith(errors.New("connection refused"))

	// A Project without an account yet had nothing made for it: the name is
	// free at once.
	s.expectError(s.open(p, "shop-assets", "10gb"), http.StatusServiceUnavailable, 63, 10010)
	s.Len(s.outboxPayloads(constants.StoragePackageStarted), 0)
	s.engine.FailWith(nil)
	s.opened(p, "shop-assets", "10gb")

	s.engine.FailWith(errors.New("connection refused"))
	s.expectError(s.open(p, "backups", "10gb"), http.StatusServiceUnavailable, 63, 10010)
	s.Len(s.manage(http.MethodGet, projectPath(p, ""), "", p.Owner).List, 1)

	s.engine.FailWith(nil)
	s.expectError(s.open(p, "backups", "10gb"), http.StatusConflict, 63, 10004)
	s.clock.Advance(11 * time.Minute)
	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	s.Equal(http.StatusCreated, s.open(p, "backups", "10gb").Code)
}

// An engine that answers but refuses is a permanent failure, not something to
// repeat, and the name is free again at once.
func (s *storageSuite) TestStorages_AnEngineThatRefusesFreesTheName() {
	p := s.grantProject()
	s.engine.RefuseWith(errors.New("bucket belongs to another account"))

	s.expectError(s.open(p, "shop-assets", "10gb"), http.StatusInternalServerError, 63, 10013)

	s.engine.RefuseWith(nil)
	s.Equal(http.StatusCreated, s.open(p, "shop-assets", "10gb").Code)
}

// A reservation left behind by a crash is freed by the reconcile job once
// nobody can be finishing it, together with whatever the engine made for it.
func (s *storageSuite) TestStorages_AnAbandonedReservationIsFreed() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.exec(`UPDATE storages SET status = 'opening', package_started_at = NULL WHERE id = $1`, id)

	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	_, held := s.engine.Bucket("shop-assets")
	s.True(held, "a fresh reservation may still be finishing")

	s.clock.Advance(11 * time.Minute)
	s.Require().NoError(s.jobs.Reconcile(s.T().Context()))
	_, held = s.engine.Bucket("shop-assets")
	s.False(held)
	s.Equal(http.StatusCreated, s.open(p, "shop-assets", "10gb").Code)
}

func (s *storageSuite) TestStorages_AProjectHoldsAtMostAHundred() {
	p := s.grantProject()
	s.opened(p, "shop-assets", "10gb")
	s.exec(`UPDATE storages SET id = gen_random_uuid()`)
	for i := 0; i < 99; i++ {
		s.exec(`INSERT INTO storages (id, project_id, team_id, region, bucket_name, status, package_code, size_bytes, monthly_price, package_started_at, created_by, created_at)
			SELECT gen_random_uuid(), project_id, team_id, region, bucket_name || $1, status, package_code, size_bytes, monthly_price, package_started_at, created_by, created_at
			FROM storages WHERE bucket_name = 'shop-assets'`, "-"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}

	s.expectError(s.open(p, "one-more", "10gb"), http.StatusConflict, 63, 10005)
}

// Two Storages of one Project share the Project's account in the engine and
// are opened in it, each on its own Package.
func (s *storageSuite) TestStorages_SeveralStoragesInOneProject() {
	p := s.grantProject()
	a := s.opened(p, "images", "10gb")
	b := s.opened(p, "backups", "250gb")

	images, _ := s.engine.Bucket("images")
	backups, _ := s.engine.Bucket("backups")
	s.Equal(images.Account, backups.Account)
	s.Equal(int64(10_000_000_000), images.SizeLimit)
	s.Equal(int64(250_000_000_000), backups.SizeLimit)
	s.NotEqual(a.str("id"), b.str("id"))
	s.Len(s.outboxPayloads(constants.StoragePackageStarted), 2)
}
