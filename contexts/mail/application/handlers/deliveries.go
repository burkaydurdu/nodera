package handlers

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/secrets"
)

const (
	webhookBatches   = 50
	webhookDue       = 20
	webhookLease     = 2 * time.Minute
	webhookUserAgent = "Nodera-Webhooks/1"
	maxErrorLength   = 500
	disabledError    = "the webhook was disabled"
)

// Deliveries carries batched Message events to the Teams' Webhooks. A
// delivery is leased while it is attempted, so a crashed worker's attempt is
// made again by the next one: Webhooks are at least once, and the
// Nodera-Delivery-Id lets a receiver drop a repeat.
type Deliveries struct {
	log          logger.ILog
	webhooks     webhook.IService
	sealer       *secrets.Sealer
	transactions postgresql.ITransactionManager
	client       *http.Client
	now          Clock
}

func NewDeliveries(log logger.ILog, webhooks webhook.IService, sealer *secrets.Sealer, transactions postgresql.ITransactionManager, allowLocal bool, now Clock) *Deliveries {
	return &Deliveries{log: log, webhooks: webhooks, sealer: sealer, transactions: transactions, client: deliveryClient(allowLocal), now: now}
}

// deliveryClient refuses to connect to a private address, checked on the
// address actually dialled so a DNS answer that changes after validation
// cannot point a Webhook inside Nodera's network. It never follows a
// redirect, for the same reason.
func deliveryClient(allowLocal bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowLocal {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil || !webhook.PublicIP(net.ParseIP(host)) {
				return webhook.ErrorPrivateTarget
			}

			return nil
		}
	}

	return &http.Client{
		Timeout:       webhook.Timeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second, MaxIdleConnsPerHost: 4},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (h *Deliveries) Run(ctx context.Context) error {
	if err := h.batch(ctx); err != nil {
		return err
	}

	due, err := h.claim(ctx)
	if err != nil {
		return err
	}
	for _, d := range due {
		if err := h.attempt(ctx, d); err != nil {
			h.log.ErrorWithFields(err, logger.NewFields().Add("delivery_id", d.Delivery.ID))
		}
	}

	return nil
}

func (h *Deliveries) batch(ctx context.Context) error {
	ids, err := h.webhooks.ListWithPending(ctx, webhookBatches)
	if err != nil {
		return err
	}

	for _, id := range ids {
		if err := h.batchOne(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

func (h *Deliveries) batchOne(ctx context.Context, webhookID uuid.UUID) error {
	ctx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(ctx) }()

	if _, err := h.webhooks.Batch(ctx, webhookID, webhook.BatchSize, h.now()); err != nil {
		return err
	}

	return h.transactions.Commit(ctx)
}

// claim takes the due deliveries and leases them before any request leaves,
// so no transaction is held across a Team's server.
func (h *Deliveries) claim(ctx context.Context) ([]*webhook.DueDelivery, error) {
	ctx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.transactions.Rollback(ctx) }()

	now := h.now()
	due, err := h.webhooks.ListDue(ctx, now, webhookDue)
	if err != nil {
		return nil, err
	}
	lease := now.Add(webhookLease)
	for _, d := range due {
		d.Delivery.NextAttemptAt = &lease
		if err := h.webhooks.RecordAttempt(ctx, d.Delivery); err != nil {
			return nil, err
		}
	}

	return due, h.transactions.Commit(ctx)
}

func (h *Deliveries) attempt(ctx context.Context, due *webhook.DueDelivery) error {
	d := due.Delivery
	now := h.now()
	if !due.Active {
		reason := disabledError
		d.Status, d.NextAttemptAt, d.LastError = webhook.StatusFailed, nil, &reason

		return h.webhooks.RecordAttempt(ctx, d)
	}

	status, failure := h.post(ctx, due, now)
	d.Attempts++
	d.LastAttemptAt = &now
	d.LastStatusCode = status
	d.LastError = failure

	if failure == nil {
		d.Status, d.NextAttemptAt = webhook.StatusSucceeded, nil
	} else if next, ok := webhook.NextAttempt(d.CreatedAt, d.Attempts, now); ok {
		d.NextAttemptAt = &next
	} else {
		d.Status, d.NextAttemptAt = webhook.StatusFailed, nil
	}

	return h.webhooks.RecordAttempt(ctx, d)
}

// post sends the frozen batch once; any answer but 2xx is a failure.
func (h *Deliveries) post(ctx context.Context, due *webhook.DueDelivery, now time.Time) (*int, *string) {
	secret, err := h.sealer.Open(due.Secret)
	if err != nil {
		return nil, failure(err.Error())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, due.URL, bytes.NewReader(due.Delivery.Body))
	if err != nil {
		return nil, failure(err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webhookUserAgent)
	req.Header.Set(webhook.SignatureHeader, webhook.Sign(secret, now.Unix(), due.Delivery.Body))
	req.Header.Set(webhook.WebhookIDHeader, due.Delivery.WebhookID.String())
	req.Header.Set(webhook.DeliveryHeader, due.Delivery.ID.String())

	res, err := h.client.Do(req)
	if err != nil {
		return nil, failure(err.Error())
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))

	code := res.StatusCode
	if code < http.StatusOK || code >= http.StatusMultipleChoices {
		return &code, failure("the webhook answered " + strconv.Itoa(code))
	}

	return &code, nil
}

func failure(text string) *string {
	if len(text) > maxErrorLength {
		text = text[:maxErrorLength]
	}

	return &text
}
