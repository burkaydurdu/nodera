package integrationtest

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/async/constants"
)

func (s *mailSuite) TestVerifyAsPublished_LetsTheProjectSendFromTheDomain() {
	p := s.grantProject()
	added := s.addDomain(p, "shop.example")
	s.Require().Equal(http.StatusOK, s.choose(p, "10k").Code)
	key := s.apiKey(p)
	s.expectError(s.sendTo(key, "hello@shop.example", "ana@example.net"),
		http.StatusUnprocessableEntity, int(message.Domain), message.ErrorSenderNotVerified.Code)

	checked, err := app.VerifyAsPublished(context.Background(), s.deps, uuid.MustParse(added.str("id")))

	s.Require().NoError(err)
	s.Equal(sendingdomain.StatusVerified, checked.Status)
	s.False(checked.DMARCFound, "only the three records the web app asks for are published")
	s.Len(s.outboxPayloads(constants.MailDomainVerified), 1, "announced as a real verification is")
	sent := s.sendTo(key, "hello@shop.example", "ana@example.net")
	s.Equal(http.StatusAccepted, sent.Code, sent.Raw)
}

func (s *mailSuite) TestVerifyAsPublished_AnUnknownSendingDomainIsNotFound() {
	_, err := app.VerifyAsPublished(context.Background(), s.deps, uuid.New())

	s.ErrorIs(err, sendingdomain.ErrorNotFound)
}
