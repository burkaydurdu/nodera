package message_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
)

func TestProgress_StatusOnlyMovesForward(t *testing.T) {
	cases := []struct {
		current, incoming, want message.EventType
	}{
		{message.EventAccepted, message.EventDeferred, message.EventDeferred},
		{message.EventDeferred, message.EventDelivered, message.EventDelivered},
		{message.EventDelivered, message.EventDeferred, message.EventDelivered},
		{message.EventDelivered, message.EventBounced, message.EventBounced},
		{message.EventDelivered, message.EventComplained, message.EventComplained},
		{message.EventComplained, message.EventDelivered, message.EventComplained},
		{message.EventSuppressed, message.EventDelivered, message.EventSuppressed},
	}

	for _, c := range cases {
		assert.Equalf(t, c.want, message.Progress(c.current, c.incoming), "%s then %s", c.current, c.incoming)
	}
}

func TestReturnPaths_NameTheMessageAndTheRecipient(t *testing.T) {
	paths := message.NewReturnPaths([]byte("return-path-test-key"))
	id := uuid.MustParse("0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d")

	address := paths.For(id, 3, "n4k2x9q1z.example.com")

	assert.Regexp(t, `^b\+0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d\.3\.[0-9a-f]{12}@n4k2x9q1z\.example\.com$`, address)
	parsedID, position, ok := paths.Parse(address)
	require.True(t, ok)
	assert.Equal(t, id, parsedID)
	assert.Equal(t, 3, position)
}

func TestReturnPaths_SurviveAServerThatChangesTheCase(t *testing.T) {
	paths := message.NewReturnPaths([]byte("return-path-test-key"))
	id := uuid.New()

	_, position, ok := paths.Parse(strings.ToUpper(paths.For(id, 1, "n4k2x9q1z.example.com")))

	require.True(t, ok)
	assert.Equal(t, 1, position)
}

func TestReturnPaths_RefuseAnotherRecipientsPositionUnderOnesTag(t *testing.T) {
	paths := message.NewReturnPaths([]byte("return-path-test-key"))
	id := uuid.New()
	mine := paths.For(id, 0, "n4k2x9q1z.example.com")

	forged := strings.Replace(mine, ".0.", ".1.", 1)

	_, _, ok := paths.Parse(forged)
	assert.False(t, ok)
}

func TestReturnPaths_RefuseOneMadeWithAnotherKey(t *testing.T) {
	theirs := message.NewReturnPaths([]byte("someone-elses-key")).For(uuid.New(), 0, "n4k2x9q1z.example.com")

	_, _, ok := message.NewReturnPaths([]byte("return-path-test-key")).Parse(theirs)

	assert.False(t, ok)
}

func TestReturnPaths_RefuseAddressesNoderaDidNotMake(t *testing.T) {
	paths := message.NewReturnPaths([]byte("return-path-test-key"))
	for _, address := range []string{
		"postmaster@example.com",
		"b+not-a-uuid.0.000000000000@example.com",
		"b+0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d.01.000000000000@example.com",
		"b+0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d.-1.000000000000@example.com",
		"b+0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d.0@example.com",
		"no-at-sign",
	} {
		_, _, ok := paths.Parse(address)
		assert.Falsef(t, ok, "%s", address)
	}
}

func TestNormalizeRecipients_DedupesAcrossKindsAndKeepsTheFirst(t *testing.T) {
	recipients, err := message.NormalizeRecipients(
		[]message.Address{{Email: "Ana@Example.com", Name: "Ana"}},
		[]message.Address{{Email: "ana@example.com"}, {Email: "bob@example.com"}},
		[]message.Address{{Email: "carl@example.com"}},
	)
	require.NoError(t, err)

	require.Len(t, recipients, 3)
	assert.Equal(t, "ana@example.com", recipients[0].Address)
	assert.Equal(t, message.KindTo, recipients[0].Kind)
	assert.Equal(t, "bob@example.com", recipients[1].Address)
	assert.Equal(t, message.KindCc, recipients[1].Kind)
	assert.Equal(t, message.KindBcc, recipients[2].Kind)
	for i, r := range recipients {
		assert.Equal(t, i, r.Position)
	}
}

func TestNormalizeRecipients_RefusesAMessageWithNobody(t *testing.T) {
	_, err := message.NormalizeRecipients(nil, nil, nil)

	assert.ErrorIs(t, err, message.ErrorNoRecipients)
}

func TestNormalizeRecipients_RefusesMoreThanFifty(t *testing.T) {
	many := make([]message.Address, 51)
	for i := range many {
		many[i] = message.Address{Email: uuid.NewString() + "@example.com"}
	}

	_, err := message.NormalizeRecipients(many, nil, nil)

	assert.ErrorIs(t, err, message.ErrorTooManyRecipients)
}

func TestNormalizeRecipients_RefusesWhatIsNotAnAddress(t *testing.T) {
	_, err := message.NormalizeRecipients([]message.Address{{Email: "not an address"}}, nil, nil)

	assert.ErrorIs(t, err, message.ErrorInvalidAddress)
}

func TestDomainOf_IsTheLowerCaseHost(t *testing.T) {
	assert.Equal(t, "example.com", message.DomainOf("No-Reply@Example.COM"))
}

func TestDomainOf_IsEmptyWithoutAnAt(t *testing.T) {
	assert.Empty(t, message.DomainOf("broken"))
}
