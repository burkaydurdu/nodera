package invoice

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

//go:generate mockgen -destination=../../mock/services/invoice.go -package=mockservices -mock_names=IService=MockIInvoiceService github.com/piggystats/nodera/contexts/billing/aggregates/invoice IService
type IService interface {
	// Issue numbers and persists an Invoice with its lines and Tax lines,
	// inside the caller's transaction.
	Issue(ctx context.Context, inv *Invoice) error
	// PeriodInvoiceExists makes Period close idempotent per Team and Period.
	PeriodInvoiceExists(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (bool, error)
	// BilledHours answers the hours of each Droplet already on a live Invoice
	// of the Period, so a later Invoice never bills them again; BilledMail
	// what live Invoices of the Period already charged for each Project's
	// Mail, so a later one charges only what is left.
	BilledHours(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]int, error)
	BilledMail(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]decimal.Decimal, error)
	FindByID(ctx context.Context, teamID, id uuid.UUID) (*Invoice, error)
	// FindAnyByID answers an Invoice of any Team, for a signed link that
	// names no Team.
	FindAnyByID(ctx context.Context, id uuid.UUID) (*Invoice, error)
	// FindForUpdate locks an Invoice of any Team for the transaction.
	FindForUpdate(ctx context.Context, id uuid.UUID) (*Invoice, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) ([]*Invoice, error)
	// FindFailed answers every payment_failed Invoice, oldest failure first;
	// FindFailedForTeam the Team's.
	FindFailed(ctx context.Context) ([]*Invoice, error)
	FindFailedForTeam(ctx context.Context, teamID uuid.UUID) ([]*Invoice, error)
	// Save writes the collection fields: status, paid and failed times and
	// the Dunning step.
	Save(ctx context.Context, inv *Invoice) error
	// SetPDFKey and SetEInvoiceRef record what was produced after the issue
	// without touching the collection fields.
	SetPDFKey(ctx context.Context, id uuid.UUID, key string) error
	SetEInvoiceRef(ctx context.Context, id uuid.UUID, ref string) error
	// SetNotified records that the InvoiceIssued email went out.
	SetNotified(ctx context.Context, id uuid.UUID, at time.Time) error
	// FindIssuedBefore answers Invoices still issued since before the
	// instant, for the sweep that charges what was never charged.
	FindIssuedBefore(ctx context.Context, before time.Time, limit int) ([]*Invoice, error)
	// UnpaidExists reports an issued or payment_failed Invoice; the last card
	// cannot be deleted while one exists.
	UnpaidExists(ctx context.Context, teamID uuid.UUID) (bool, error)
	CreditBalance(ctx context.Context, teamID uuid.UUID) (decimal.Decimal, error)
	GrantCredit(ctx context.Context, teamID uuid.UUID, amount decimal.Decimal, reason string) error
}

type IRepository interface {
	NextNumber(ctx context.Context, year int) (int64, error)
	Create(ctx context.Context, inv *Invoice) error
	ApplyCredit(ctx context.Context, teamID, invoiceID uuid.UUID, amount decimal.Decimal) error
	PeriodInvoiceExists(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (bool, error)
	BilledHours(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]int, error)
	BilledMail(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]decimal.Decimal, error)
	Find(ctx context.Context, id uuid.UUID, teamID *uuid.UUID, lock bool) (*Invoice, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) ([]*Invoice, error)
	FindFailed(ctx context.Context, teamID *uuid.UUID) ([]*Invoice, error)
	Save(ctx context.Context, inv *Invoice) error
	SetPDFKey(ctx context.Context, id uuid.UUID, key string) error
	SetEInvoiceRef(ctx context.Context, id uuid.UUID, ref string) error
	SetNotified(ctx context.Context, id uuid.UUID, at time.Time) error
	FindIssuedBefore(ctx context.Context, before time.Time, limit int) ([]*Invoice, error)
	UnpaidExists(ctx context.Context, teamID uuid.UUID) (bool, error)
	CreditBalance(ctx context.Context, teamID uuid.UUID) (decimal.Decimal, error)
	GrantCredit(ctx context.Context, teamID uuid.UUID, amount decimal.Decimal, reason string) error
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

// Issue allocates the number from the year of the issue date and records the
// Credit application next to the Invoice.
func (s *Service) Issue(ctx context.Context, inv *Invoice) error {
	sequence, err := s.repository.NextNumber(ctx, inv.IssuedAt.UTC().Year())
	if err != nil {
		return err
	}
	inv.Number = FormatNumber(inv.IssuedAt.UTC().Year(), sequence)
	if err := s.repository.Create(ctx, inv); err != nil {
		return err
	}
	if inv.CreditApplied.IsPositive() {
		return s.repository.ApplyCredit(ctx, inv.TeamID, inv.ID, inv.CreditApplied)
	}

	return nil
}

func (s *Service) PeriodInvoiceExists(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (bool, error) {
	return s.repository.PeriodInvoiceExists(ctx, teamID, periodStart)
}

func (s *Service) BilledHours(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]int, error) {
	return s.repository.BilledHours(ctx, teamID, periodStart)
}

func (s *Service) BilledMail(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]decimal.Decimal, error) {
	return s.repository.BilledMail(ctx, teamID, periodStart)
}

func (s *Service) FindByID(ctx context.Context, teamID, id uuid.UUID) (*Invoice, error) {
	return s.repository.Find(ctx, id, &teamID, false)
}

func (s *Service) FindAnyByID(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	return s.repository.Find(ctx, id, nil, false)
}

func (s *Service) FindForUpdate(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	return s.repository.Find(ctx, id, nil, true)
}

func (s *Service) FindByTeamID(ctx context.Context, teamID uuid.UUID) ([]*Invoice, error) {
	return s.repository.FindByTeamID(ctx, teamID)
}

func (s *Service) FindFailed(ctx context.Context) ([]*Invoice, error) {
	return s.repository.FindFailed(ctx, nil)
}

func (s *Service) FindFailedForTeam(ctx context.Context, teamID uuid.UUID) ([]*Invoice, error) {
	return s.repository.FindFailed(ctx, &teamID)
}

func (s *Service) Save(ctx context.Context, inv *Invoice) error {
	return s.repository.Save(ctx, inv)
}

func (s *Service) SetPDFKey(ctx context.Context, id uuid.UUID, key string) error {
	return s.repository.SetPDFKey(ctx, id, key)
}

func (s *Service) SetEInvoiceRef(ctx context.Context, id uuid.UUID, ref string) error {
	return s.repository.SetEInvoiceRef(ctx, id, ref)
}

func (s *Service) SetNotified(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.repository.SetNotified(ctx, id, at)
}

func (s *Service) FindIssuedBefore(ctx context.Context, before time.Time, limit int) ([]*Invoice, error) {
	return s.repository.FindIssuedBefore(ctx, before, limit)
}

func (s *Service) UnpaidExists(ctx context.Context, teamID uuid.UUID) (bool, error) {
	return s.repository.UnpaidExists(ctx, teamID)
}

func (s *Service) CreditBalance(ctx context.Context, teamID uuid.UUID) (decimal.Decimal, error) {
	return s.repository.CreditBalance(ctx, teamID)
}

func (s *Service) GrantCredit(ctx context.Context, teamID uuid.UUID, amount decimal.Decimal, reason string) error {
	return s.repository.GrantCredit(ctx, teamID, amount, reason)
}
