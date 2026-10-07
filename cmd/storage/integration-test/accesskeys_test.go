package integrationtest

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/async/constants"
	"github.com/piggystats/nodera/contexts/storage/engine"
)

func (s *storageSuite) createKey(p project, id, name, permission string, u user) *response {
	return s.manage(http.MethodPost, storagePath(p, id, "/access-keys"), fmt.Sprintf(`{"name":%q,"permission":%q}`, name, permission), u)
}

// The secret key is in the answer that makes the key and nowhere else; the
// engine holds a key that reaches only this Storage with what it may do.
func (s *storageSuite) TestAccessKeys_MadeOnceShownOnce() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	created := s.createKey(p, id, "CI deploy", "read_write", p.Owner)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.Equal("CI deploy", created.str("name"))
	s.Equal("read_write", created.str("permission"))
	s.NotEmpty(created.str("access_key_id"))
	s.NotEmpty(created.str("secret_key"))
	s.Equal(p.Owner.ID.String(), created.str("created_by"))

	bucket, _ := s.engine.Bucket("shop-assets")
	s.Equal(map[uuid.UUID]engine.Permission{uuid.MustParse(created.str("id")): engine.ReadWrite}, bucket.Keys)

	listed := s.manage(http.MethodGet, storagePath(p, id, "/access-keys"), "", p.Owner)
	s.Require().Equal(http.StatusOK, listed.Code, listed.Raw)
	s.Require().Len(listed.List, 1)
	s.Equal(created.str("access_key_id"), listed.List[0]["access_key_id"])
	_, shown := listed.List[0]["secret_key"]
	s.False(shown, "the secret key is never listed")
}

func (s *storageSuite) TestAccessKeys_RevokedKeysLeaveTheEngine() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	key := s.createKey(p, id, "ci", "read", p.Owner)

	s.Equal(http.StatusNoContent, s.manage(http.MethodDelete, storagePath(p, id, "/access-keys/"+key.str("id")), "", p.Owner).Code)

	bucket, _ := s.engine.Bucket("shop-assets")
	s.Empty(bucket.Keys)
	s.Len(s.manage(http.MethodGet, storagePath(p, id, "/access-keys"), "", p.Owner).List, 0)
	s.expectError(s.manage(http.MethodDelete, storagePath(p, id, "/access-keys/"+key.str("id")), "", p.Owner), http.StatusNotFound, 64, 10001)
}

// An engine that cannot be reached revokes nothing: the key stays listed so
// the Team knows it is not yet gone.
func (s *storageSuite) TestAccessKeys_AnUnreachableEngineKeepsTheKeyListed() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	key := s.createKey(p, id, "ci", "read", p.Owner)
	s.engine.FailWith(errors.New("timeout"))

	s.expectError(s.manage(http.MethodDelete, storagePath(p, id, "/access-keys/"+key.str("id")), "", p.Owner), http.StatusServiceUnavailable, 63, 10010)
	s.expectError(s.createKey(p, id, "two", "read", p.Owner), http.StatusServiceUnavailable, 63, 10010)
	s.Len(s.manage(http.MethodGet, storagePath(p, id, "/access-keys"), "", p.Owner).List, 1)
}

func (s *storageSuite) TestAccessKeys_AKeyOutlivesTheMemberWhoMadeIt() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	developer := s.member(p.TeamID, "developer")
	key := s.createKey(p, id, "ci", "read_write", developer)
	s.Require().Equal(http.StatusCreated, key.Code, key.Raw)

	s.consume(constants.TeamMemberRemoved, fmt.Sprintf(`{"payload":{"team_id":%q,"user_id":%q,"removed_at":%q}}`,
		p.TeamID, developer.ID, time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)))

	bucket, _ := s.engine.Bucket("shop-assets")
	s.Len(bucket.Keys, 1)
	s.Len(s.manage(http.MethodGet, storagePath(p, id, "/access-keys"), "", p.Owner).List, 1)
}

func (s *storageSuite) TestAccessKeys_AStorageHoldsAtMostTwenty() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")
	for i := range 20 {
		s.Require().Equal(http.StatusCreated, s.createKey(p, id, fmt.Sprintf("key-%d", i), "read", p.Owner).Code)
	}

	s.expectError(s.createKey(p, id, "one-more", "read", p.Owner), http.StatusConflict, 64, 10002)
}

func (s *storageSuite) TestAccessKeys_PermissionIsReadOrReadWrite() {
	p := s.grantProject()
	id := s.opened(p, "shop-assets", "10gb").str("id")

	s.Equal(http.StatusBadRequest, s.createKey(p, id, "ci", "admin", p.Owner).Code)
}
