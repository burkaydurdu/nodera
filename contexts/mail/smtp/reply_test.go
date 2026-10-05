package smtp

import (
	"errors"
	"testing"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/mail/aggregates/billing"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/apikey"
)

// The SMTP answers keep the API's split: what can clear by itself is 4xx so
// the client's MTA retries, what needs the Team to act is 5xx.
func TestReply(t *testing.T) {
	cases := []struct {
		err      error
		code     int
		enhanced gosmtp.EnhancedCode
	}{
		{billing.ErrorTeamSuspended, 451, gosmtp.EnhancedCode{4, 7, 1}},
		{limits.ErrorPaused, 451, gosmtp.EnhancedCode{4, 7, 1}},
		{limits.ErrorDailyLimitReached.Copy().Add("retry_after_seconds", 10), 452, gosmtp.EnhancedCode{4, 5, 3}},
		{packages.ErrorPackageExhausted, 452, gosmtp.EnhancedCode{4, 5, 3}},
		{packages.ErrorNotEnoughAllowance, 452, gosmtp.EnhancedCode{4, 5, 3}},
		{packages.ErrorNoPackage, 550, gosmtp.EnhancedCode{5, 7, 1}},
		{message.ErrorSenderNotVerified, 550, gosmtp.EnhancedCode{5, 7, 1}},
		{message.ErrorMalformedMessage, 554, gosmtp.EnhancedCode{5, 6, 0}},
		{message.ErrorTooManyRecipients, 550, gosmtp.EnhancedCode{5, 5, 3}},
		{message.ErrorInvalidAddress, 553, gosmtp.EnhancedCode{5, 1, 3}},
		{apikey.ErrorUnavailable, 454, gosmtp.EnhancedCode{4, 7, 0}},
		{errors.New("database is gone"), 451, gosmtp.EnhancedCode{4, 3, 0}},
	}

	for _, c := range cases {
		reply, _ := Reply(c.err)
		assert.Equalf(t, c.code, reply.Code, "%v", c.err)
		assert.Equalf(t, c.enhanced, reply.EnhancedCode, "%v", c.err)
	}
}

func TestReply_KnowsARefusalOfTheSendPath(t *testing.T) {
	_, refused := Reply(limits.ErrorPaused)

	assert.True(t, refused)
}

func TestReply_AnswersNoderaFailingAsTemporary(t *testing.T) {
	reply, refused := Reply(errors.New("database is gone"))

	assert.False(t, refused)
	assert.Equal(t, 451, reply.Code)
}

func TestLoginServer_AsksForBothHalves(t *testing.T) {
	var gotUser, gotPassword string
	server := newLoginServer(func(user, password string) error {
		gotUser, gotPassword = user, password

		return nil
	})

	challenge, done, err := server.Next(nil)
	assert.NoError(t, err)
	assert.False(t, done)
	assert.Equal(t, "Username:", string(challenge))

	challenge, done, err = server.Next([]byte("apikey"))
	assert.NoError(t, err)
	assert.False(t, done)
	assert.Equal(t, "Password:", string(challenge))

	_, done, err = server.Next([]byte("nd_key.secret"))
	assert.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "apikey", gotUser)
	assert.Equal(t, "nd_key.secret", gotPassword)
}

func TestLoginServer_TakesTheUserAsInitialResponse(t *testing.T) {
	server := newLoginServer(func(string, string) error { return nil })

	challenge, done, err := server.Next([]byte("apikey"))
	assert.NoError(t, err)
	assert.False(t, done)
	assert.Equal(t, "Password:", string(challenge))
}
