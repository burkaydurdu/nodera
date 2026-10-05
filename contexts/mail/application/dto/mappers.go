package dto

import (
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/aggregates/suppression"
	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/contexts/mail/paging"
)

func ToPackages(list []*packages.Package) []Package {
	out := make([]Package, 0, len(list))
	for _, p := range list {
		out = append(out, Package{Code: p.Code, MonthlyEmails: p.MonthlyEmails, MonthlyPrice: p.MonthlyPrice.StringFixed(packages.PriceScale)})
	}

	return out
}

func ToSendingDomain(d *sendingdomain.SendingDomain, zone string) *SendingDomain {
	out := &SendingDomain{
		ID: d.ID, Name: d.Name, Status: string(d.Status), DMARCFound: d.DMARCFound,
		VerifiedAt: d.VerifiedAt, LastCheckedAt: d.LastCheckedAt, CreatedAt: d.CreatedAt,
	}
	for _, r := range d.Records(zone) {
		out.Records = append(out.Records, Record{
			Purpose: string(r.Purpose), Type: r.Type, Host: r.Host, Value: r.Value, Valid: r.Valid, CheckedAt: r.CheckedAt,
		})
	}

	return out
}

func ToSendingDomains(list sendingdomain.SendingDomains, zone string) []*SendingDomain {
	out := make([]*SendingDomain, 0, len(list))
	for _, d := range list {
		out = append(out, ToSendingDomain(d, zone))
	}

	return out
}

func ToMessagePage(list []*message.Message, next *paging.Cursor) *MessagePage {
	page := &MessagePage{Items: make([]MessageSummary, 0, len(list))}
	for _, m := range list {
		summary := MessageSummary{ID: m.ID, From: m.From.Email, Subject: m.Subject, Tags: m.Tags, Source: string(m.Source), CreatedAt: m.CreatedAt}
		for _, r := range m.Recipients {
			summary.Recipients = append(summary.Recipients, RecipientSummary{Address: r.Address, Status: string(r.Status)})
		}
		page.Items = append(page.Items, summary)
	}
	if next != nil {
		cursor := next.Encode()
		page.NextCursor = &cursor
	}

	return page
}

func ToMessageDetail(m *message.Message) *MessageDetail {
	out := &MessageDetail{
		ID: m.ID, From: m.From.Email, Subject: m.Subject, MessageIDHeader: m.MessageIDHeader, Tags: m.Tags,
		Source: string(m.Source), SizeBytes: m.SizeBytes, CreatedAt: m.CreatedAt,
	}
	for _, r := range m.Recipients {
		detail := RecipientDetail{Address: r.Address, Kind: string(r.Kind), Status: string(r.Status), Events: []Event{}}
		for _, e := range r.Events {
			event := Event{Type: string(e.Type), OccurredAt: e.OccurredAt}
			if e.Response != nil {
				event.Response = &Response{Code: e.Response.Code, EnhancedCode: e.Response.EnhancedCode, Text: e.Response.Text}
			}
			detail.Events = append(detail.Events, event)
		}
		out.Recipients = append(out.Recipients, detail)
	}

	return out
}

func ToSuppressionPage(list []*suppression.Suppression, next *string) *SuppressionPage {
	page := &SuppressionPage{Items: make([]Suppression, 0, len(list)), NextCursor: next}
	for _, s := range list {
		page.Items = append(page.Items, Suppression{ID: s.ID, Address: s.Address, Reason: string(s.Reason), MessageID: s.MessageID, CreatedAt: s.CreatedAt})
	}

	return page
}

func ToWebhook(w *webhook.Webhook) *Webhook {
	return &Webhook{ID: w.ID, URL: w.URL, Events: w.Events, Active: w.Active, CreatedAt: w.CreatedAt}
}

func ToWebhooks(list []*webhook.Webhook) []*Webhook {
	out := make([]*Webhook, 0, len(list))
	for _, w := range list {
		out = append(out, ToWebhook(w))
	}

	return out
}

func ToDeliveryPage(list []*webhook.Delivery, next *string) *DeliveryPage {
	page := &DeliveryPage{Items: make([]Delivery, 0, len(list)), NextCursor: next}
	for _, d := range list {
		page.Items = append(page.Items, Delivery{
			ID: d.ID, Status: string(d.Status), Attempts: d.Attempts, EventCount: d.EventCount, LastStatusCode: d.LastStatusCode,
			LastError: d.LastError, NextAttemptAt: d.NextAttemptAt, CreatedAt: d.CreatedAt, LastAttemptAt: d.LastAttemptAt,
		})
	}

	return page
}

func ToSent(m *message.Message) *Sent {
	return &Sent{ID: m.ID, Accepted: m.Accepted(), Suppressed: m.Suppressed()}
}
