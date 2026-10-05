package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/billing"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/aggregates/suppression"
	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/contexts/mail/apikey"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

// Content builds a Message's final bytes once its id is known, and answers
// the Message-ID header they carry.
type Content func(c ContentContext) ([]byte, string, error)

// ContentContext is what Nodera adds to every Message.
type ContentContext struct {
	MessageIDHeader string
	Date            time.Time
	FeedbackID      string
}

// SendInput is one Message from the HTTP API or the SMTP relay, already
// parsed; Recipients are normalized.
type SendInput struct {
	Key            *apikey.Key
	Source         message.Source
	From           message.Address
	Recipients     []*message.Recipient
	Subject        string
	Tags           []string
	IdempotencyKey string
	Content        Content
}

// Sender is the one send path of the HTTP API and the SMTP relay.
type Sender struct {
	log          logger.ILog
	access       access.IService
	billing      billing.IService
	domains      sendingdomain.IService
	packages     packages.IService
	limits       limits.IService
	messages     message.IService
	suppressions suppression.IService
	webhooks     webhook.IService
	periods      *periods
	transactions postgresql.ITransactionManager
	dispatcher   *Dispatcher
	announcer    Announcer
	returnPaths  message.ReturnPaths
	now          Clock
}

func NewSender(
	log logger.ILog,
	accessService access.IService,
	billingService billing.IService,
	domains sendingdomain.IService,
	packageService packages.IService,
	limitService limits.IService,
	messages message.IService,
	suppressions suppression.IService,
	webhooks webhook.IService,
	transactions postgresql.ITransactionManager,
	dispatcher *Dispatcher,
	announcer Announcer,
	returnPaths message.ReturnPaths,
	now Clock,
) *Sender {
	return &Sender{
		log: log, access: accessService, billing: billingService, domains: domains, packages: packageService,
		limits: limitService, messages: messages, suppressions: suppressions, webhooks: webhooks,
		periods:      &periods{access: accessService, packages: packageService, announcer: announcer},
		transactions: transactions, dispatcher: dispatcher, announcer: announcer, returnPaths: returnPaths, now: now,
	}
}

// Send accepts a Message or refuses it whole, checking in the spec's order:
// the sender's Sending domain, a repeated Idempotency-Key, the Team's
// Billing status, the Package, the Pause, the Suppressions and then the
// allowance and the Daily limit, counted atomically. An accepted Message is handed
// to the MTA right after the commit; the dispatcher job retries what was not.
func (h *Sender) Send(ctx context.Context, in SendInput) (*message.Message, error) {
	if err := validateTags(in.Tags); err != nil {
		return nil, err
	}

	project, err := h.access.FindProject(ctx, in.Key.ProjectID)
	if err != nil {
		return nil, err
	}

	sender, err := message.NormalizeAddress(in.From.Email)
	if err != nil {
		return nil, err
	}
	sd, err := h.domains.FindVerifiedByName(ctx, project.ProjectID, message.DomainOf(sender))
	if errors.Is(err, sendingdomain.ErrorNotFound) {
		return nil, message.ErrorSenderNotVerified
	}
	if err != nil {
		return nil, err
	}

	var out announcements
	m, stored, err := h.accept(ctx, project, sd, sender, in, &out)
	if err != nil {
		return nil, err
	}
	out.publish(ctx, h.log)

	if stored && m.Accepted() > 0 {
		if err := h.dispatcher.DispatchOne(ctx, m.ID); err != nil {
			h.log.ErrorWithFields(err, logger.NewFields().Add("message_id", m.ID))
		}
	}

	return m, nil
}

// accept answers the Message and whether this call stored it; a repeated
// Idempotency-Key answers the first Message, which needs no new hand-over.
func (h *Sender) accept(ctx context.Context, project *access.ProjectAccess, sd *sendingdomain.SendingDomain, sender string, in SendInput, out *announcements) (*message.Message, bool, error) {
	ctx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = h.transactions.Rollback(ctx) }()

	now := h.now()
	if str.IsNotEmpty(in.IdempotencyKey) {
		if err := h.messages.LockIdempotencyKey(ctx, project.ProjectID, in.IdempotencyKey); err != nil {
			return nil, false, err
		}
		earlier, err := h.messages.FindByIdempotencyKey(ctx, project.ProjectID, in.IdempotencyKey, now.Add(-message.IdempotencyWindow))
		if err != nil {
			return nil, false, err
		}
		if earlier != nil {
			return earlier, false, h.transactions.Commit(ctx)
		}
	}

	if err := h.requireSending(ctx, project, now, out); err != nil {
		return nil, false, err
	}

	suppressed, err := h.suppressedAmong(ctx, project.ProjectID, in.Recipients)
	if err != nil {
		return nil, false, err
	}
	sending := len(in.Recipients) - len(suppressed)
	if sending > 0 {
		if err := h.count(ctx, project, sending, now, out); err != nil {
			return nil, false, err
		}
	}

	m, err := h.store(ctx, project, sd, sender, in, suppressed, now)
	if err != nil {
		return nil, false, err
	}

	return m, true, h.transactions.Commit(ctx)
}

// requireSending is the order of refusals a Project can be in.
func (h *Sender) requireSending(ctx context.Context, project *access.ProjectAccess, now time.Time, out *announcements) error {
	status, err := h.billing.StatusOf(ctx, project.TeamID)
	if err != nil {
		return err
	}
	if status == billing.StatusSuspended {
		return billing.ErrorTeamSuspended
	}

	pp, _, err := h.periods.ensure(ctx, project.ProjectID, now, out)
	if err != nil {
		return err
	}
	if pp == nil || pp.Current == nil {
		return packages.ErrorNoPackage
	}

	lim, err := h.limits.Ensure(ctx, project.ProjectID, project.TeamID)
	if err != nil {
		return err
	}
	if lim.Paused() {
		return limits.ErrorPaused
	}

	return nil
}

func (h *Sender) suppressedAmong(ctx context.Context, projectID uuid.UUID, recipients []*message.Recipient) (map[string]bool, error) {
	addresses := make([]string, len(recipients))
	for i, r := range recipients {
		addresses[i] = r.Address
	}

	return h.suppressions.Suppressed(ctx, projectID, addresses)
}

// count takes n emails from the Period's allowance and today's Daily limit
// together, in the order the overview reports them; the transaction gives
// both back if either refuses.
func (h *Sender) count(ctx context.Context, project *access.ProjectAccess, n int, now time.Time, out *announcements) error {
	start := packages.PeriodStart(now)
	before, after, ok, err := h.packages.AddUsage(ctx, project.ProjectID, start, n)
	if err != nil {
		return err
	}
	if !ok {
		return h.allowanceRefusal(ctx, project.ProjectID, start, now)
	}

	lim, err := h.limits.Find(ctx, project.ProjectID)
	if err != nil {
		return err
	}
	today := limits.DayOf(now)
	ok, err = h.limits.ConsumeDaily(ctx, project.ProjectID, today, lim.DailyLimit, n)
	if err != nil {
		return err
	}
	if !ok {
		return limits.ErrorDailyLimitReached.Copy().Add("retry_after_seconds", retryAfter(now, today.AddDate(0, 0, 1)))
	}

	return h.announceUsage(ctx, project, start, before, after, now, out)
}

// allowanceRefusal tells a used-up allowance from one with too little left
// for this Message: smaller Messages still fit the latter, so the client
// learns how many emails are left.
func (h *Sender) allowanceRefusal(ctx context.Context, projectID uuid.UUID, start, now time.Time) error {
	period, err := h.packages.FindPeriod(ctx, projectID, start)
	if err != nil {
		return err
	}
	wait := retryAfter(now, packages.NextPeriodStart(now))
	if period == nil || period.Exhausted() {
		return packages.ErrorPackageExhausted.Copy().Add("retry_after_seconds", wait)
	}

	return packages.ErrorNotEnoughAllowance.Copy().
		SetMessagef("the package has %d emails left this period, fewer than the message has recipients", period.Left()).
		Add("retry_after_seconds", wait)
}

func (h *Sender) announceUsage(ctx context.Context, project *access.ProjectAccess, start time.Time, before, after int, now time.Time, out *announcements) error {
	period, err := h.packages.FindPeriod(ctx, project.ProjectID, start)
	if err != nil || period == nil {
		return err
	}
	pp, err := h.packages.FindProject(ctx, project.ProjectID)
	if err != nil || pp == nil || pp.Current == nil {
		return err
	}

	near, exhausted := period.Crossed(before, after)
	if near {
		if marked, err := h.packages.MarkNearLimitNotified(ctx, project.ProjectID, start, now); err != nil {
			return err
		} else if marked {
			snapshot, code := *period, pp.Current.Code
			out.add(func(ctx context.Context) error {
				return h.announcer.PackageNearLimit(ctx, project, code, &snapshot, now)
			})
		}
	}
	if exhausted {
		if marked, err := h.packages.MarkExhaustedNotified(ctx, project.ProjectID, start, now); err != nil {
			return err
		} else if marked {
			snapshot, code := *period, pp.Current.Code
			out.add(func(ctx context.Context) error {
				return h.announcer.PackageExhausted(ctx, project, code, &snapshot, now)
			})
			out.add(func(ctx context.Context) error {
				return h.announcer.SendingChanged(ctx, project.TeamID, project.ProjectID, now)
			})
		}
	}

	return nil
}

// store builds the content, records the Message with an accepted or a
// suppressed event per recipient, queues them for the Webhooks and spools
// the content for the MTA.
func (h *Sender) store(
	ctx context.Context, project *access.ProjectAccess, sd *sendingdomain.SendingDomain, sender string,
	in SendInput, suppressed map[string]bool, now time.Time,
) (*message.Message, error) {
	id := uuid.New()
	content, messageIDHeader, err := in.Content(ContentContext{
		MessageIDHeader: id.String() + "@" + sd.Name,
		Date:            now,
		FeedbackID:      fmt.Sprintf("%s:%s:mail:nodera", project.ProjectID, project.TeamID),
	})
	if err != nil {
		return nil, message.ErrorMalformedMessage.Copy().SetError(err)
	}

	m := &message.Message{
		ID: id, ProjectID: project.ProjectID, TeamID: project.TeamID, SendingDomainID: sd.ID, APIKeyID: in.Key.ID, Source: in.Source,
		From: message.Address{Email: sender, Name: in.From.Name}, Subject: truncate(in.Subject), MessageIDHeader: messageIDHeader,
		Tags: in.Tags, SizeBytes: len(content), CreatedAt: now, Recipients: in.Recipients,
	}
	if m.Tags == nil {
		m.Tags = []string{}
	}
	if str.IsNotEmpty(in.IdempotencyKey) {
		m.IdempotencyKey = &in.IdempotencyKey
	}

	envelope := message.Envelope{ProjectID: project.ProjectID, TeamID: project.TeamID, SendingDomainID: sd.ID, DKIMDomain: sd.Name}
	for _, r := range m.Recipients {
		status := message.EventAccepted
		if suppressed[r.Address] {
			status = message.EventSuppressed
		} else {
			envelope.Recipients = append(envelope.Recipients, message.EnvelopeRecipient{
				Email: r.Address, Position: r.Position, ReturnPath: h.returnPaths.For(id, r.Position, sd.ReturnPathDomain()),
			})
		}
		r.Status, r.UpdatedAt = status, now
		r.Events = []*message.Event{{ID: uuid.New(), MessageID: id, Position: r.Position, Type: status, OccurredAt: now}}
	}

	if err := h.messages.Create(ctx, m); err != nil {
		return nil, err
	}
	for _, r := range m.Recipients {
		payload, err := webhookPayload(m, r.Address, r.Events[0])
		if err != nil {
			return nil, err
		}
		if err := h.webhooks.Enqueue(ctx, project.ProjectID, string(r.Status), payload, now); err != nil {
			return nil, err
		}
	}

	if len(envelope.Recipients) == 0 {
		return m, nil
	}

	// The job waits a grace period before touching a fresh Message, so it is
	// the immediate attempt after the commit that hands it over.
	return m, h.messages.Spool(ctx, &message.SpoolEntry{
		MessageID: id, Content: content, Envelope: envelope, NextAttemptAt: now.Add(message.SpoolBackoff[0]), CreatedAt: now,
	})
}

func validateTags(tags []string) error {
	if len(tags) > message.MaxTags {
		return message.ErrorInvalidTag
	}
	for _, t := range tags {
		if str.IsBlank(t) || utf8.RuneCountInString(t) > message.MaxTagLength {
			return message.ErrorInvalidTag
		}
	}

	return nil
}

// truncate keeps a log's subject readable; the Message itself carries the
// subject as sent.
func truncate(subject string) string {
	const max = 998
	subject = strings.TrimSpace(subject)
	if utf8.RuneCountInString(subject) <= max {
		return subject
	}

	return string([]rune(subject)[:max])
}
