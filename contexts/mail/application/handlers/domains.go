package handlers

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/publicsuffix"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/contexts/mail/secrets"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
)

const tokenAttempts = 3

// Resolver is the DNS a Sending domain is checked against.
//
//go:generate mockgen -destination=../../mock/handlers/resolver.go -package=mockhandlers github.com/piggystats/nodera/contexts/mail/application/handlers Resolver
type Resolver interface {
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// SendingDomains is the Sending domain: adding one with its keys, checking its
// records and handing its signing key to the MTA.
type SendingDomains struct {
	authorizer
	log          logger.ILog
	domains      sendingdomain.IService
	transactions postgresql.ITransactionManager
	sealer       *secrets.Sealer
	resolver     Resolver
	announcer    Announcer
	zone         string
	now          Clock
}

func NewSendingDomains(
	log logger.ILog,
	accessService access.IService,
	domains sendingdomain.IService,
	transactions postgresql.ITransactionManager,
	sealer *secrets.Sealer,
	resolver Resolver,
	announcer Announcer,
	zone string,
	now Clock,
) *SendingDomains {
	return &SendingDomains{
		authorizer: authorizer{access: accessService}, log: log, domains: domains, transactions: transactions,
		sealer: sealer, resolver: resolver, announcer: announcer, zone: zone, now: now,
	}
}

func (h *SendingDomains) List(ctx context.Context, req *sendingdomain.ListRequest) ([]*dto.SendingDomain, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailRead); err != nil {
		return nil, err
	}

	list, err := h.domains.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return dto.ToSendingDomains(list, h.zone), nil
}

func (h *SendingDomains) Get(ctx context.Context, req *sendingdomain.GetRequest) (*dto.SendingDomain, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailRead); err != nil {
		return nil, err
	}

	d, err := h.domains.Find(ctx, projectID, uuid.MustParse(req.SendingDomainID))
	if err != nil {
		return nil, err
	}

	return dto.ToSendingDomain(d, h.zone), nil
}

// Add creates the Sending domain with a fresh token and both keys. Its
// records cannot exist yet, so the first check waits for the usual interval.
func (h *SendingDomains) Add(ctx context.Context, req *sendingdomain.AddRequest) (*dto.SendingDomain, error) {
	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.project(ctx, projectID, authz.MailWrite)
	if err != nil {
		return nil, err
	}

	name, err := sendingdomain.NormalizeName(req.Name, h.zone)
	if err != nil {
		return nil, err
	}

	count, err := h.domains.CountByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if count >= sendingdomain.MaxPerProject {
		return nil, sendingdomain.ErrorTooMany
	}

	for range tokenAttempts {
		d, err := h.create(ctx, projectID, membership.TeamID, name)
		if errors.Is(err, sendingdomain.ErrorTokenCollision) {
			continue
		}
		if err != nil {
			return nil, err
		}

		return dto.ToSendingDomain(d, h.zone), nil
	}

	return nil, sendingdomain.ErrorTokenCollision
}

func (h *SendingDomains) create(ctx context.Context, projectID, teamID uuid.UUID, name string) (*sendingdomain.SendingDomain, error) {
	token, err := sendingdomain.NewToken()
	if err != nil {
		return nil, err
	}

	now := h.now()
	d := &sendingdomain.SendingDomain{
		ID: uuid.New(), ProjectID: projectID, TeamID: teamID, Name: name, Token: token, Status: sendingdomain.StatusPending,
		ActiveSelector: sendingdomain.SelectorA, NextCheckAt: now.Add(sendingdomain.PendingInterval), KeysRotatedAt: now, CreatedAt: now,
	}

	keys := make([]*sendingdomain.Key, 0, 2)
	for _, selector := range []sendingdomain.Selector{sendingdomain.SelectorA, sendingdomain.SelectorB} {
		key, err := h.newKey(d.ID, selector)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	ctx, err = h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.transactions.Rollback(ctx) }()

	if err := h.domains.Create(ctx, d, keys); err != nil {
		return nil, err
	}

	return d, h.transactions.Commit(ctx)
}

func (h *SendingDomains) newKey(domainID uuid.UUID, selector sendingdomain.Selector) (*sendingdomain.Key, error) {
	pair, err := sendingdomain.GenerateKey()
	if err != nil {
		return nil, err
	}
	sealed, err := h.sealer.Seal(pair.PrivatePKCS8)
	if err != nil {
		return nil, err
	}

	return &sendingdomain.Key{SendingDomainID: domainID, Selector: selector, PrivateKey: sealed, PublicKey: pair.PublicKey}, nil
}

// Delete stops sending from the domain at once; the logs of what it sent stay
// until they expire.
func (h *SendingDomains) Delete(ctx context.Context, req *sendingdomain.GetRequest) error {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailWrite); err != nil {
		return err
	}

	return h.domains.Delete(ctx, projectID, uuid.MustParse(req.SendingDomainID))
}

// Verify is "verify now": a check at once, at most once a minute.
func (h *SendingDomains) Verify(ctx context.Context, req *sendingdomain.GetRequest) (*dto.SendingDomain, error) {
	projectID := uuid.MustParse(req.ProjectID)
	if _, err := h.project(ctx, projectID, authz.MailWrite); err != nil {
		return nil, err
	}

	d, err := h.domains.Find(ctx, projectID, uuid.MustParse(req.SendingDomainID))
	if err != nil {
		return nil, err
	}
	if !d.CanVerifyNow(h.now()) {
		return nil, sendingdomain.ErrorVerifyTooSoon.Copy().Add("retry_after_seconds", retryAfter(h.now(), d.LastCheckedAt.Add(sendingdomain.VerifyCooldown)))
	}

	checked, err := h.Check(ctx, d.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToSendingDomain(checked, h.zone), nil
}

// Check looks the domain's records up, records what it found and announces a
// change of status. The job and "verify now" both go through it. DNS is asked
// before the transaction opens, so a slow resolver never holds the row lock.
func (h *SendingDomains) Check(ctx context.Context, domainID uuid.UUID) (*sendingdomain.SendingDomain, error) {
	found, err := h.domains.FindByID(ctx, domainID)
	if err != nil {
		return nil, err
	}
	result := h.lookup(ctx, found)

	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	d, err := h.domains.FindForUpdate(txCtx, domainID)
	if err != nil {
		return nil, err
	}
	now := h.now()
	change := d.ApplyCheck(result, now)
	if err := h.domains.Save(txCtx, d); err != nil {
		return nil, err
	}
	if change != sendingdomain.ChangeNone {
		if err := h.announceChange(txCtx, d, change, now, &out); err != nil {
			return nil, err
		}
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}
	out.publish(ctx, h.log)

	return d, nil
}

func (h *SendingDomains) lookup(ctx context.Context, d *sendingdomain.SendingDomain) sendingdomain.CheckResult {
	var result sendingdomain.CheckResult
	var answered [3]bool
	result.ReturnPath, answered[0] = h.pointsAt(ctx, d.ReturnPathDomain(), sendingdomain.ReturnPathTarget(h.zone))
	result.DKIMA, answered[1] = h.pointsAt(ctx, d.DKIMHost(sendingdomain.SelectorA), d.KeyHost(sendingdomain.SelectorA, h.zone))
	result.DKIMB, answered[2] = h.pointsAt(ctx, d.DKIMHost(sendingdomain.SelectorB), d.KeyHost(sendingdomain.SelectorB, h.zone))
	result.Unanswered = !answered[0] || !answered[1] || !answered[2]

	// DMARC only warns, so a lookup without an answer keeps what the last
	// one found instead of holding the check back.
	dmarc, dmarcAnswered := h.hasDMARC(ctx, d.Name)
	result.DMARC = dmarc || (!dmarcAnswered && d.DMARCFound)

	return result
}

// pointsAt answers whether host is a CNAME to expected, and whether DNS
// answered at all: a name it says does not exist is an answer, a timeout or
// a SERVFAIL is not.
func (h *SendingDomains) pointsAt(ctx context.Context, host, expected string) (points, answered bool) {
	target, err := h.resolver.LookupCNAME(ctx, host)
	if err != nil {
		return false, isNotFound(err)
	}

	return strings.EqualFold(strings.TrimSuffix(target, "."), expected), true
}

// hasDMARC looks on the domain and then on its organizational domain, where a
// subdomain's policy usually lives.
func (h *SendingDomains) hasDMARC(ctx context.Context, name string) (found, answered bool) {
	names := []string{name}
	if org, err := publicsuffix.EffectiveTLDPlusOne(name); err == nil && org != name {
		names = append(names, org)
	}

	answered = true
	for _, n := range names {
		records, err := h.resolver.LookupTXT(ctx, "_dmarc."+n)
		if err != nil {
			answered = answered && isNotFound(err)

			continue
		}
		for _, r := range records {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r)), "v=dmarc1") {
				return true, true
			}
		}
	}

	return false, answered
}

func isNotFound(err error) bool {
	var dnsErr *net.DNSError

	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

func (h *SendingDomains) announceChange(ctx context.Context, d *sendingdomain.SendingDomain, change sendingdomain.Change, at time.Time, out *announcements) error {
	project, err := h.access.FindProject(ctx, d.ProjectID)
	if err != nil {
		return err
	}

	snapshot := *d
	if change == sendingdomain.ChangeVerified {
		out.add(func(ctx context.Context) error { return h.announcer.DomainVerified(ctx, project, &snapshot, at) })
	} else {
		out.add(func(ctx context.Context) error { return h.announcer.DomainUnverified(ctx, project, &snapshot, at) })
	}
	out.add(func(ctx context.Context) error {
		return h.announcer.DomainChanged(ctx, snapshot.TeamID, snapshot.ProjectID, snapshot.ID, snapshot.Status, at)
	})

	return nil
}

// SigningKey is what KumoMTA signs with: the active selector's key in PEM. It
// answers whatever the domain's status, because a Message accepted before a
// domain lost its records must still leave signed.
func (h *SendingDomains) SigningKey(ctx context.Context, req *sendingdomain.KeyRequest) (*dto.DKIMKey, error) {
	d, err := h.domains.FindByID(ctx, uuid.MustParse(req.SendingDomainID))
	if err != nil {
		return nil, err
	}

	key, err := h.domains.FindKey(ctx, d.ID, d.ActiveSelector)
	if err != nil {
		return nil, err
	}
	der, err := h.sealer.Open(key.PrivateKey)
	if err != nil {
		return nil, err
	}

	return &dto.DKIMKey{
		Domain:        d.Name,
		Selector:      d.SigningSelector(),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	}, nil
}

// Rotate publishes a fresh key under the idle selector of a domain due for
// rotation; Promote signs with it once the wait has passed (docs/adr/0011).
func (h *SendingDomains) Rotate(ctx context.Context, domainID uuid.UUID) error {
	ctx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(ctx) }()

	d, err := h.domains.FindForUpdate(ctx, domainID)
	if err != nil {
		return err
	}
	if d.RotationSelector != nil {
		return nil
	}

	idle := d.ActiveSelector.Other()
	key, err := h.newKey(d.ID, idle)
	if err != nil {
		return err
	}
	if err := h.domains.ReplaceKey(ctx, key); err != nil {
		return err
	}

	ready := h.now().Add(sendingdomain.RotationWait)
	d.RotationSelector, d.RotationReadyAt = &idle, &ready
	if err := h.domains.Save(ctx, d); err != nil {
		return err
	}

	return h.transactions.Commit(ctx)
}

func (h *SendingDomains) Promote(ctx context.Context, domainID uuid.UUID) error {
	ctx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(ctx) }()

	d, err := h.domains.FindForUpdate(ctx, domainID)
	if err != nil {
		return err
	}
	if d.RotationSelector == nil {
		return nil
	}

	d.ActiveSelector = *d.RotationSelector
	d.RotationSelector, d.RotationReadyAt = nil, nil
	d.KeysRotatedAt = h.now()
	if err := h.domains.Save(ctx, d); err != nil {
		return err
	}

	return h.transactions.Commit(ctx)
}
