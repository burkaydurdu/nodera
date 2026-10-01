package integrationtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
)

// ed25519Key answers a fresh authorized_keys line and its ssh-keygen fingerprint.
func (s *customerSuite) ed25519Key(comment string) (line, fingerprint string) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	s.Require().NoError(err)
	sshPub, err := ssh.NewPublicKey(pub)
	s.Require().NoError(err)
	line = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}

	return line, ssh.FingerprintSHA256(sshPub)
}

func (s *customerSuite) rsaKey(bits int) string {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	s.Require().NoError(err)
	sshPub, err := ssh.NewPublicKey(&key.PublicKey)
	s.Require().NoError(err)

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

func sshKeyBody(name, publicKey string) string {
	return fmt.Sprintf(`{"name":%q,"public_key":%q}`, name, publicKey)
}

func (s *customerSuite) TestSSHKeys_CreateListDelete() {
	tokens := s.registerAndVerify(s.uniqueEmail("sshkeys"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)
	line, fingerprint := s.ed25519Key("ada@laptop")

	empty := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/ssh-keys", "", tokens.Access)
	s.Equal(http.StatusOK, empty.Code, empty.Raw)
	s.Len(empty.List, 0)

	created := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("  Laptop  ", "  "+line+"\n"), tokens.Access)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)
	s.Equal("Laptop", created.str("name"))
	s.Equal(teamID, created.str("team_id"))
	s.Equal(line, created.str("public_key"))
	s.Equal(fingerprint, created.str("fingerprint"))
	s.NotEmpty(created.str("id"))
	s.NotEmpty(created.str("created_at"))
	_, hasCreatedBy := created.Body["created_by"]
	s.False(hasCreatedBy, "created_by is not part of the contract")

	list := s.authed(http.MethodGet, "/v1/teams/"+teamID+"/ssh-keys", "", tokens.Access)
	s.Require().Len(list.List, 1)
	s.Equal(created.str("id"), list.List[0]["id"])
	s.Equal(fingerprint, list.List[0]["fingerprint"])

	deleted := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/ssh-keys/"+created.str("id"), "", tokens.Access)
	s.Equal(http.StatusNoContent, deleted.Code, deleted.Raw)
	s.Empty(deleted.Raw)

	s.Len(s.authed(http.MethodGet, "/v1/teams/"+teamID+"/ssh-keys", "", tokens.Access).List, 0)

	again := s.authed(http.MethodDelete, "/v1/teams/"+teamID+"/ssh-keys/"+created.str("id"), "", tokens.Access)
	s.Equal(sshkey.ErrorNotFound.HttpStatus, again.Code, again.Raw)
	_, code := again.errorCode()
	s.Equal(sshkey.ErrorNotFound.Code, code)
}

func (s *customerSuite) TestSSHKeys_RSA2048_IsAccepted() {
	tokens := s.registerAndVerify(s.uniqueEmail("rsa2048"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("rsa", s.rsaKey(2048)), tokens.Access)

	s.Equal(http.StatusCreated, res.Code, res.Raw)
	s.True(strings.HasPrefix(res.str("fingerprint"), "SHA256:"), res.Raw)
}

func (s *customerSuite) TestSSHKeys_RSA1024_IsRejected() {
	tokens := s.registerAndVerify(s.uniqueEmail("rsa1024"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("weak", s.rsaKey(1024)), tokens.Access)

	s.Equal(sshkey.ErrorKeyTooShort.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusBadRequest, res.Code)
	domain, code := res.errorCode()
	s.Equal(int(sshkey.Domain), domain)
	s.Equal(sshkey.ErrorKeyTooShort.Code, code)
}

func (s *customerSuite) TestSSHKeys_InvalidKey_IsRejected() {
	tokens := s.registerAndVerify(s.uniqueEmail("badkey"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("junk", "ssh-ed25519 notbase64!!! x"), tokens.Access)

	s.Equal(sshkey.ErrorInvalidPublicKey.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusBadRequest, res.Code)
	_, code := res.errorCode()
	s.Equal(sshkey.ErrorInvalidPublicKey.Code, code)
}

func (s *customerSuite) TestSSHKeys_BlankNameOrMissingKey_IsRejected() {
	tokens := s.registerAndVerify(s.uniqueEmail("blankkey"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)
	line, _ := s.ed25519Key("")

	blank := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("   ", line), tokens.Access)
	s.Equal(http.StatusBadRequest, blank.Code, blank.Raw)

	tooLong := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody(strings.Repeat("k", 65), line), tokens.Access)
	s.Equal(http.StatusBadRequest, tooLong.Code, tooLong.Raw)

	missing := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", `{"name":"ok"}`, tokens.Access)
	s.Equal(http.StatusBadRequest, missing.Code, missing.Raw)
}

func (s *customerSuite) TestSSHKeys_DuplicateName_IsAConflict() {
	tokens := s.registerAndVerify(s.uniqueEmail("dupname"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)
	first, _ := s.ed25519Key("")
	second, _ := s.ed25519Key("")

	s.Require().Equal(http.StatusCreated, s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("Laptop", first), tokens.Access).Code)

	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("laptop", second), tokens.Access)

	s.Equal(sshkey.ErrorNameTaken.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusConflict, res.Code)
	_, code := res.errorCode()
	s.Equal(sshkey.ErrorNameTaken.Code, code)
}

func (s *customerSuite) TestSSHKeys_DuplicateFingerprint_IsAConflict() {
	tokens := s.registerAndVerify(s.uniqueEmail("dupfp"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)
	line, _ := s.ed25519Key("first-comment")

	s.Require().Equal(http.StatusCreated, s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("one", line), tokens.Access).Code)

	// Same key, different name and comment: the fingerprint is what counts.
	sameKey := strings.Join(strings.Fields(line)[:2], " ") + " another-comment"
	res := s.authed(http.MethodPost, "/v1/teams/"+teamID+"/ssh-keys", sshKeyBody("two", sameKey), tokens.Access)

	s.Equal(sshkey.ErrorFingerprintTaken.HttpStatus, res.Code, res.Raw)
	s.Equal(http.StatusConflict, res.Code)
	_, code := res.errorCode()
	s.Equal(sshkey.ErrorFingerprintTaken.Code, code)
}

func (s *customerSuite) TestSSHKeys_SameKeyInAnotherTeam_IsAllowed() {
	tokens := s.registerAndVerify(s.uniqueEmail("twoteams"), "Str0ng!pass")
	firstTeam := s.teamID(tokens.Access)
	secondTeam := s.authed(http.MethodPost, "/v1/teams", `{"name":"Second"}`, tokens.Access).str("id")
	line, _ := s.ed25519Key("")

	s.Equal(http.StatusCreated, s.authed(http.MethodPost, "/v1/teams/"+firstTeam+"/ssh-keys", sshKeyBody("k", line), tokens.Access).Code)
	s.Equal(http.StatusCreated, s.authed(http.MethodPost, "/v1/teams/"+secondTeam+"/ssh-keys", sshKeyBody("k", line), tokens.Access).Code)
}

func (s *customerSuite) TestSSHKeys_StrangerSeesNotFound() {
	owner := s.registerAndVerify(s.uniqueEmail("keyowner"), "Str0ng!pass")
	ownerTeam := s.teamID(owner.Access)
	line, _ := s.ed25519Key("")
	created := s.authed(http.MethodPost, "/v1/teams/"+ownerTeam+"/ssh-keys", sshKeyBody("mine", line), owner.Access)
	s.Require().Equal(http.StatusCreated, created.Code, created.Raw)

	stranger := s.registerAndVerify(s.uniqueEmail("keystranger"), "Str0ng!pass")
	strangerLine, _ := s.ed25519Key("")

	list := s.authed(http.MethodGet, "/v1/teams/"+ownerTeam+"/ssh-keys", "", stranger.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, list.Code, list.Raw)
	_, code := list.errorCode()
	s.Equal(team.ErrorNotMember.Code, code)

	create := s.authed(http.MethodPost, "/v1/teams/"+ownerTeam+"/ssh-keys", sshKeyBody("sneaky", strangerLine), stranger.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, create.Code, create.Raw)

	del := s.authed(http.MethodDelete, "/v1/teams/"+ownerTeam+"/ssh-keys/"+created.str("id"), "", stranger.Access)
	s.Equal(team.ErrorNotMember.HttpStatus, del.Code, del.Raw)

	// The key is untouched; a stranger's own Team cannot reach it by id either.
	s.Len(s.authed(http.MethodGet, "/v1/teams/"+ownerTeam+"/ssh-keys", "", owner.Access).List, 1)
	strangerTeam := s.teamID(stranger.Access)
	cross := s.authed(http.MethodDelete, "/v1/teams/"+strangerTeam+"/ssh-keys/"+created.str("id"), "", stranger.Access)
	s.Equal(sshkey.ErrorNotFound.HttpStatus, cross.Code, cross.Raw)
}

func (s *customerSuite) TestSSHKeys_WithoutSession_IsUnauthorized() {
	tokens := s.registerAndVerify(s.uniqueEmail("nosession"), "Str0ng!pass")
	teamID := s.teamID(tokens.Access)

	res := s.request(http.MethodGet, "/v1/teams/"+teamID+"/ssh-keys", "", "")

	s.Equal(http.StatusUnauthorized, res.Code, res.Raw)
}
