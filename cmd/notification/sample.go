package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/cmd/notification/config"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/realtime"
)

// sampleAll sends every sample in one run.
const sampleAll = "all"

// sampleFrontURL is where the samples' email links point; the command runs in
// dev only, against the web app on its dev port.
const sampleFrontURL = "http://localhost:3002"

// sample is one Notification type and the event that makes it, shaped as the
// context that owns the event publishes it.
type sample struct {
	routingKey string
	payload    func(teamID uuid.UUID, now time.Time) map[string]any
}

// samples are the Notification types about a Team. Every run names new ids,
// so a sample is never mistaken for a redelivery of the previous one.
var samples = map[string]sample{
	"droplet_active": {constants.DropletStatusChanged, func(teamID uuid.UUID, now time.Time) map[string]any {
		return dropletChange(teamID, now, "active", map[string]any{})
	}},
	"droplet_failed": {constants.DropletStatusChanged, func(teamID uuid.UUID, now time.Time) map[string]any {
		return dropletChange(teamID, now, "failed", map[string]any{"failure_code": "rejected", "failure_reason": "the server request was refused"})
	}},
	"invoice_issued": {constants.InvoiceIssued, func(teamID uuid.UUID, now time.Time) map[string]any {
		return invoice(teamID, map[string]any{
			"period_start": monthStart(now).AddDate(0, -1, 0), "period_end": monthStart(now).Add(-time.Second),
			"total_due": "42.00", "currency": "USD", "pdf_url": sampleFrontURL + "/dashboard/team/billing",
		})
	}},
	"payment_succeeded": {constants.PaymentSucceeded, func(teamID uuid.UUID, now time.Time) map[string]any {
		return payment(teamID, map[string]any{"paid_at": now})
	}},
	"payment_failed": {constants.PaymentFailed, func(teamID uuid.UUID, now time.Time) map[string]any {
		return payment(teamID, map[string]any{"failure_reason": "insufficient funds", "attempt": 1, "next_retry_at": now.Add(48 * time.Hour)})
	}},
	"payment_confirmation_required": {constants.PaymentConfirmationRequired, func(teamID uuid.UUID, _ time.Time) map[string]any {
		return payment(teamID, map[string]any{"confirm_url": sampleFrontURL + "/dashboard/team/billing"})
	}},
	"team_suspended": {constants.TeamSuspended, func(teamID uuid.UUID, _ time.Time) map[string]any {
		return invoice(teamID, map[string]any{"pay_url": sampleFrontURL + "/dashboard/team/billing"})
	}},
	"droplets_deletion_warning": {constants.DropletsDeletionWarning, func(teamID uuid.UUID, now time.Time) map[string]any {
		return invoice(teamID, map[string]any{
			"deletion_at": now.Add(7 * 24 * time.Hour), "dunning_day": 14, "pay_url": sampleFrontURL + "/dashboard/team/billing",
		})
	}},
	"card_expiring": {constants.CardExpiring, func(teamID uuid.UUID, now time.Time) map[string]any {
		next := now.AddDate(0, 1, 0)

		return map[string]any{
			"team_id": teamID, "payment_method_id": uuid.New(), "brand": "visa", "last4": "4242",
			"exp_month": int(next.Month()), "exp_year": next.Year(),
		}
	}},
	"mail_domain_verified": {constants.MailDomainVerified, func(teamID uuid.UUID, now time.Time) map[string]any {
		return mailProject(teamID, map[string]any{"sending_domain_id": uuid.New(), "domain": "mail.example.com", "verified_at": now})
	}},
	"mail_domain_unverified": {constants.MailDomainUnverified, func(teamID uuid.UUID, now time.Time) map[string]any {
		return mailProject(teamID, map[string]any{"sending_domain_id": uuid.New(), "domain": "mail.example.com", "unverified_at": now})
	}},
	"mail_package_near_limit": {constants.MailPackageNearLimit, func(teamID uuid.UUID, now time.Time) map[string]any {
		return mailUsage(teamID, now, 8000)
	}},
	"mail_package_exhausted": {constants.MailPackageExhausted, func(teamID uuid.UUID, now time.Time) map[string]any {
		return mailUsage(teamID, now, 10000)
	}},
	"mail_rates_warning": {constants.MailRatesWarning, func(teamID uuid.UUID, now time.Time) map[string]any {
		return mailProject(teamID, map[string]any{"bounce_rate": "2.40", "complaint_rate": "0.02", "window_days": 7, "at": now})
	}},
	"mail_paused": {constants.MailSendingPaused, func(teamID uuid.UUID, now time.Time) map[string]any {
		return mailProject(teamID, map[string]any{"reason": "bounce_rate", "bounce_rate": "5.40", "complaint_rate": "0.02", "paused_at": now})
	}},
}

// sampleTypes lists the types in a stable order, for the help and the errors.
func sampleTypes() []string {
	types := make([]string, 0, len(samples))
	for t := range samples {
		types = append(types, t)
	}
	slices.Sort(types)

	return types
}

// sampleEvent only registers the common exchange: the samples are maps.
type sampleEvent struct{}

// RunSample publishes the event behind a sample Notification for a Team, signed
// like any event, so the worker, the dispatcher, the socket and the web app all
// run as they would for the real thing. The samples name things that do not
// exist, so opening one in the web app finds nothing.
func RunSample(conf *config.Config, notificationType string, rawTeamID string) {
	log := logger.New(conf.Environment)
	if !conf.IsDev() {
		log.Fatal(fmt.Errorf("sample notifications are for dev only, not %q", conf.Environment))
	}
	teamID, err := uuid.Parse(strings.TrimSpace(rawTeamID))
	if err != nil {
		log.Fatal(fmt.Errorf("--team must be a Team id: %w", err))
	}

	types := []string{notificationType}
	if notificationType == sampleAll {
		types = sampleTypes()
	} else if _, ok := samples[notificationType]; !ok {
		log.Fatal(fmt.Errorf("unknown --type %q; one of %s or %s", notificationType, strings.Join(sampleTypes(), ", "), sampleAll))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newRabbitMQClient(ctx, conf, log)
	client.AddPublisher(ctx, constants.CommonExchange, rabbitmq.Topic, sampleEvent{}, realtime.Message{})

	now := time.Now().UTC()
	for _, t := range types {
		s := samples[t]
		publishCtx := context.WithValue(ctx, rabbitmq.CorrelationID, uuid.NewString())
		if err := client.Publish(publishCtx, s.routingKey, constants.CommonExchange, map[string]any{"payload": s.payload(teamID, now)}); err != nil {
			log.Fatal(fmt.Errorf("publish %s: %w", t, err))
		}
		log.Infof("sent a sample %s for team %s", t, teamID)
	}
}

func dropletChange(teamID uuid.UUID, now time.Time, status string, extra map[string]any) map[string]any {
	dropletID := uuid.New()
	payload := map[string]any{
		"droplet_id": dropletID, "team_id": teamID, "project_id": uuid.New(),
		"hostname": "sample-" + dropletID.String()[:6], "ipv4": fmt.Sprintf("203.0.113.%d", int(dropletID[15])%250+1),
		"from_status": "provisioning", "status": status, "changed_at": now,
	}
	for k, v := range extra {
		payload[k] = v
	}

	return payload
}

func invoice(teamID uuid.UUID, extra map[string]any) map[string]any {
	invoiceID := uuid.New()
	payload := map[string]any{
		"team_id": teamID, "invoice_id": invoiceID, "invoice_number": "NOD-SAMPLE-" + strings.ToUpper(invoiceID.String()[:6]),
	}
	for k, v := range extra {
		payload[k] = v
	}

	return payload
}

func payment(teamID uuid.UUID, extra map[string]any) map[string]any {
	payload := invoice(teamID, map[string]any{"payment_id": uuid.New(), "amount": "42.00", "currency": "USD"})
	for k, v := range extra {
		payload[k] = v
	}

	return payload
}

func mailProject(teamID uuid.UUID, extra map[string]any) map[string]any {
	projectID := uuid.New()
	payload := map[string]any{"team_id": teamID, "project_id": projectID, "project_name": "sample-" + projectID.String()[:6]}
	for k, v := range extra {
		payload[k] = v
	}

	return payload
}

func mailUsage(teamID uuid.UUID, now time.Time, used int) map[string]any {
	return mailProject(teamID, map[string]any{
		"package_code": "10k", "allowance": 10000, "used": used, "period": now.Format("2006-01"), "at": now,
	})
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
