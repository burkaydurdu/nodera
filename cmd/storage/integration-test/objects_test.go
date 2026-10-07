package integrationtest

import (
	"net/http"
	"time"
)

func (s *storageSuite) TestObjects_FoldersArePagedAndWalked() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.Put("shop-assets", "readme.txt", 12, start)
	s.engine.Put("shop-assets", "images/a.png", 100, start)
	s.engine.Put("shop-assets", "images/b.png", 200, start)
	s.engine.Put("shop-assets", "backups/2026/oct.tar", 3000, start)

	root := s.manage(http.MethodGet, storagePath(p, id, "/objects"), "", p.Owner)
	s.Require().Equal(http.StatusOK, root.Code, root.Raw)
	s.Equal("", root.str("prefix"))
	s.Equal([]any{"backups/", "images/"}, root.Body["folders"])
	objects, _ := root.Body["objects"].([]any)
	s.Require().Len(objects, 1)
	s.Equal("readme.txt", objects[0].(map[string]any)["key"])
	s.Equal(float64(12), objects[0].(map[string]any)["size"])
	s.Nil(root.Body["next_cursor"])

	images := s.manage(http.MethodGet, storagePath(p, id, "/objects?prefix=images"), "", p.Owner)
	s.Require().Equal(http.StatusOK, images.Code, images.Raw)
	s.Equal("images/", images.str("prefix"))
	s.Equal([]any{}, images.Body["folders"])
	objects, _ = images.Body["objects"].([]any)
	s.Len(objects, 2)

	for i := range 250 {
		s.engine.Put("shop-assets", "many/"+time.Duration(i).String()+".bin", 1, start)
	}
	first := s.manage(http.MethodGet, storagePath(p, id, "/objects?prefix=many/"), "", p.Owner)
	s.Require().Equal(http.StatusOK, first.Code, first.Raw)
	objects, _ = first.Body["objects"].([]any)
	s.Len(objects, 200)
	s.Require().NotNil(first.Body["next_cursor"])
	second := s.manage(http.MethodGet, storagePath(p, id, "/objects?prefix=many/&cursor="+first.str("next_cursor")), "", p.Owner)
	s.Require().Equal(http.StatusOK, second.Code, second.Raw)
	objects, _ = second.Body["objects"].([]any)
	s.Len(objects, 50)
	s.Nil(second.Body["next_cursor"])
}

func (s *storageSuite) TestObjects_URLsAreSignedForTheBrowser() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	download := s.manage(http.MethodPost, storagePath(p, id, "/objects/download-url"), `{"key":"images/a.png"}`, p.Owner)
	s.Require().Equal(http.StatusOK, download.Code, download.Raw)
	s.Equal("GET", download.str("method"))
	s.Equal("https://fake.storage.test/shop-assets/images/a.png?op=get&ttl=300", download.str("url"))
	s.sameInstant(start.Add(5*time.Minute).Format(time.RFC3339Nano), download.Body["expires_at"])

	upload := s.manage(http.MethodPost, storagePath(p, id, "/objects/upload-url"), `{"key":"images/new.png","content_type":"image/png"}`, p.Owner)
	s.Require().Equal(http.StatusOK, upload.Code, upload.Raw)
	s.Equal("PUT", upload.str("method"))
	s.Equal("https://fake.storage.test/shop-assets/images/new.png?op=put&ttl=900&content-type=image/png", upload.str("url"))
	s.Equal("image/png", upload.str("content_type"), "the browser sends exactly what was signed")

	untyped := s.manage(http.MethodPost, storagePath(p, id, "/objects/upload-url"), `{"key":"images/raw.bin"}`, p.Owner)
	s.Require().Equal(http.StatusOK, untyped.Code, untyped.Raw)
	s.Nil(untyped.Body["content_type"])

	s.expectError(s.manage(http.MethodPost, storagePath(p, id, "/objects/upload-url"), `{"key":"images/"}`, p.Owner), http.StatusBadRequest, 65, 10002)
}

func (s *storageSuite) TestObjects_DeleteRemovesTheFiles() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.Put("shop-assets", "a.txt", 1, start)
	s.engine.Put("shop-assets", "b.txt", 1, start)
	s.engine.Put("shop-assets", "c.txt", 1, start)

	s.Equal(http.StatusNoContent, s.manage(http.MethodPost, storagePath(p, id, "/objects/delete"), `{"keys":["a.txt","b.txt"]}`, p.Owner).Code)

	bucket, _ := s.engine.Bucket("shop-assets")
	s.Len(bucket.Objects, 1)
	s.Equal(http.StatusBadRequest, s.manage(http.MethodPost, storagePath(p, id, "/objects/delete"), `{"keys":[]}`, p.Owner).Code)
}

// The Team's own CORS rules can drop Nodera's; one request puts it back.
func (s *storageSuite) TestObjects_BrowserAccessCanBeRestored() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	bucket, _ := s.engine.Bucket("shop-assets")
	s.Equal(1, bucket.BrowserRules, "opening allows the browser")

	s.engine.ReplaceBrowserRules("shop-assets")
	s.Equal(http.StatusNoContent, s.manage(http.MethodPut, storagePath(p, id, "/browser-access"), "", p.Owner).Code)
	bucket, _ = s.engine.Bucket("shop-assets")
	s.Equal(1, bucket.BrowserRules)
}

func (s *storageSuite) TestObjects_AnUnreachableEngineIsAnswered503() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.FailWith(errUnreachable)

	s.expectError(s.manage(http.MethodGet, storagePath(p, id, "/objects"), "", p.Owner), http.StatusServiceUnavailable, 63, 10010)
}

// An upload is private; the browser's switch lets anyone read one file and
// takes that back. Reading the switch needs storage.read, moving it
// storage.write.
func (s *storageSuite) TestObjects_AFileCanBePublishedForReading() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	s.engine.Put("shop-assets", "images/logo.png", 10, start)
	finance := s.member(p.TeamID, "finance")

	access := s.manage(http.MethodGet, storagePath(p, id, "/objects/access?key=images/logo.png"), "", finance)
	s.Require().Equal(http.StatusOK, access.Code, access.Raw)
	s.Equal(false, access.Body["public"])
	s.Equal("https://shop-assets.vno1.storage.test/images/logo.png", access.str("url"))

	s.expectError(s.manage(http.MethodPut, storagePath(p, id, "/objects/access"), `{"key":"images/logo.png","public":true}`, finance), http.StatusForbidden, 60, 10004)

	published := s.manage(http.MethodPut, storagePath(p, id, "/objects/access"), `{"key":"images/logo.png","public":true}`, p.Owner)
	s.Require().Equal(http.StatusOK, published.Code, published.Raw)
	s.Equal(true, published.Body["public"])
	bucket, _ := s.engine.Bucket("shop-assets")
	s.True(bucket.Public["images/logo.png"])

	taken := s.manage(http.MethodPut, storagePath(p, id, "/objects/access"), `{"key":"images/logo.png","public":false}`, p.Owner)
	s.Require().Equal(http.StatusOK, taken.Code, taken.Raw)
	bucket, _ = s.engine.Bucket("shop-assets")
	s.False(bucket.Public["images/logo.png"])

	s.expectError(s.manage(http.MethodGet, storagePath(p, id, "/objects/access?key=missing.png"), "", p.Owner), http.StatusNotFound, 65, 10003)
	s.expectError(s.manage(http.MethodPut, storagePath(p, id, "/objects/access"), `{"key":"missing.png","public":true}`, p.Owner), http.StatusNotFound, 65, 10003)
}
