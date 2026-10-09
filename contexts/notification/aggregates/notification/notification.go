// Package notification is the Notification: one message to one Recipient
// about something that happened, recorded for good and delivered over the
// Channels its type fixes.
package notification

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

type Type string

const (
	TypeDropletActive Type = "droplet_active"
	TypeDropletFailed Type = "droplet_failed"

	TypeInvoiceIssued               Type = "invoice_issued"
	TypePaymentSucceeded            Type = "payment_succeeded"
	TypePaymentFailed               Type = "payment_failed"
	TypePaymentConfirmationRequired Type = "payment_confirmation_required"
	TypeTeamSuspended               Type = "team_suspended"
	TypeDropletsDeletionWarning     Type = "droplets_deletion_warning"
	TypeCardExpiring                Type = "card_expiring"

	TypeMailDomainVerified   Type = "mail_domain_verified"
	TypeMailDomainUnverified Type = "mail_domain_unverified"
	TypeMailPackageNearLimit Type = "mail_package_near_limit"
	TypeMailPackageExhausted Type = "mail_package_exhausted"
	TypeMailRatesWarning     Type = "mail_rates_warning"
	TypeMailPaused           Type = "mail_paused"

	TypeStorageNearlyFull       Type = "storage_nearly_full"
	TypeStorageFull             Type = "storage_full"
	TypeStoragePaused           Type = "storage_paused"
	TypeStorageDowngradeSkipped Type = "storage_downgrade_skipped"

	TypeKubernetesClusterRunning Type = "kubernetes_cluster_running"
	TypeKubernetesClusterFailed  Type = "kubernetes_cluster_failed"

	TypeTeamInvitation    Type = "team_invitation"
	TypeTeamRoleChanged   Type = "team_role_changed"
	TypeTeamMemberRemoved Type = "team_member_removed"

	TypeEmailVerificationCode  Type = "email_verification_code"
	TypeEmailAlreadyRegistered Type = "email_already_registered"
	TypeMFAEmailCode           Type = "mfa_email_code"
	TypePasswordReset          Type = "password_reset"
)

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelInApp Channel = "in_app"
)

// Definition is what a Notification type fixes: the Channels that carry it
// and, for one about a Team, the Permission a member needs to be told
// (docs/adr/0008). It is the same Permission that lets them see the thing.
type Definition struct {
	Channels   []Channel
	Permission authz.Permission
}

var (
	inApp        = []Channel{ChannelInApp}
	inAppByEmail = []Channel{ChannelInApp, ChannelEmail}
	byEmail      = []Channel{ChannelEmail}
)

// catalog is the one table of Notification types. A type absent from it
// cannot be recorded. A type about a Team names the Permission a member needs
// to be told; one for a single person names none.
var catalog = map[Type]Definition{
	TypeDropletActive: {Channels: inApp, Permission: authz.DropletRead},
	// A failure may come long after the customer left the page, so it also
	// goes by email.
	TypeDropletFailed: {Channels: inAppByEmail, Permission: authz.DropletRead},

	// Money matters reach whoever may see the Team's billing, by email too,
	// since they ask for action.
	TypeInvoiceIssued:               {Channels: inAppByEmail, Permission: authz.BillingRead},
	TypePaymentSucceeded:            {Channels: inAppByEmail, Permission: authz.BillingRead},
	TypePaymentFailed:               {Channels: inAppByEmail, Permission: authz.BillingRead},
	TypePaymentConfirmationRequired: {Channels: inAppByEmail, Permission: authz.BillingRead},
	TypeTeamSuspended:               {Channels: inAppByEmail, Permission: authz.BillingRead},
	TypeDropletsDeletionWarning:     {Channels: inAppByEmail, Permission: authz.BillingRead},
	TypeCardExpiring:                {Channels: inAppByEmail, Permission: authz.BillingRead},

	// A Project's Mail news reaches whoever may see its Mail, finance too since
	// the Package is a line on its Invoice, and goes by email as well: most of
	// it asks for action (fix the DNS, upgrade, clean a list) while nobody may
	// be looking.
	TypeMailDomainVerified:   {Channels: inAppByEmail, Permission: authz.MailRead},
	TypeMailDomainUnverified: {Channels: inAppByEmail, Permission: authz.MailRead},
	TypeMailPackageNearLimit: {Channels: inAppByEmail, Permission: authz.MailRead},
	TypeMailPackageExhausted: {Channels: inAppByEmail, Permission: authz.MailRead},
	TypeMailRatesWarning:     {Channels: inAppByEmail, Permission: authz.MailRead},
	TypeMailPaused:           {Channels: inAppByEmail, Permission: authz.MailRead},

	// A Storage's news reaches whoever may see it, finance too since its
	// Package is a line on the Invoice, and goes by email as well: a full or
	// Paused Storage breaks the software writing to it while nobody may be
	// looking.
	TypeStorageNearlyFull:       {Channels: inAppByEmail, Permission: authz.StorageRead},
	TypeStorageFull:             {Channels: inAppByEmail, Permission: authz.StorageRead},
	TypeStoragePaused:           {Channels: inAppByEmail, Permission: authz.StorageRead},
	TypeStorageDowngradeSkipped: {Channels: inAppByEmail, Permission: authz.StorageRead},

	// A Cluster's news reaches whoever may see Kubernetes; a failure goes by
	// email too, since it may come long after the customer left the page.
	TypeKubernetesClusterRunning: {Channels: inApp, Permission: authz.KubernetesRead},
	TypeKubernetesClusterFailed:  {Channels: inAppByEmail, Permission: authz.KubernetesRead},

	// An invitation reaches an address that may have no account yet; the
	// Inbox shows it to one that has.
	TypeTeamInvitation:    {Channels: inAppByEmail},
	TypeTeamRoleChanged:   {Channels: inApp},
	TypeTeamMemberRemoved: {Channels: inApp},

	// Sign-in mail carries a code or a link that is never recorded; the
	// Notification keeps only that it was sent.
	TypeEmailVerificationCode:  {Channels: byEmail},
	TypeEmailAlreadyRegistered: {Channels: byEmail},
	TypeMFAEmailCode:           {Channels: byEmail},
	TypePasswordReset:          {Channels: byEmail},
}

func DefinitionOf(t Type) (Definition, bool) {
	d, ok := catalog[t]

	return d, ok
}

func (d Definition) Has(c Channel) bool {
	for _, channel := range d.Channels {
		if channel == c {
			return true
		}
	}

	return false
}

// Recipient is a User, or only an email address for someone who is not one.
// Only a User has an Inbox.
type Recipient struct {
	UserID   *uuid.UUID
	Email    string
	FullName string
	Locale   string
}

// Key tells one Recipient from another for the same event, so a redelivery
// records nothing twice.
func (r Recipient) Key() string {
	if r.UserID != nil {
		return "user:" + r.UserID.String()
	}

	return "email:" + strings.ToLower(strings.TrimSpace(r.Email))
}

type Notification struct {
	ID uuid.UUID
	// SourceKey names what happened, the same for every Recipient of it.
	SourceKey string
	Type      Type
	TeamID    *uuid.UUID
	Recipient Recipient
	Params    map[string]any
	InInbox   bool
	CreatedAt time.Time
	ReadAt    *time.Time
}

// Pending is a recorded Notification with the Channels it has not gone out
// on yet.
type Pending struct {
	Notification *Notification
	Channels     []Channel
}

// DefaultPageSize is an Inbox page when the caller names no limit; the
// request caps it at 50.
const DefaultPageSize = 20
