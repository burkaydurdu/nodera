package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/application/handlers"
	mockhandlers "github.com/piggystats/nodera/contexts/billing/mock/handlers"
	mockservices "github.com/piggystats/nodera/contexts/billing/mock/services"
	"github.com/piggystats/nodera/logger"
	mockpersistence "github.com/piggystats/nodera/mock/persistence"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/constants"
)

var fixedNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

type deleteFixture struct {
	access       *mockservices.MockIAccessService
	methods      *mockservices.MockIPaymentMethodService
	statuses     *mockservices.MockIBillingStatusService
	transactions *mockpersistence.MockITransactionManager
	publisher    *mockhandlers.MockStatusPublisher
	invoices     *mockhandlers.MockUnpaidInvoices
	handler      *handlers.PaymentMethod
	ctx          context.Context
	teamID       uuid.UUID
	userID       uuid.UUID
}

func newDeleteFixture(t *testing.T) *deleteFixture {
	ctrl := gomock.NewController(t)
	f := &deleteFixture{
		access:       mockservices.NewMockIAccessService(ctrl),
		methods:      mockservices.NewMockIPaymentMethodService(ctrl),
		statuses:     mockservices.NewMockIBillingStatusService(ctrl),
		transactions: mockpersistence.NewMockITransactionManager(ctrl),
		publisher:    mockhandlers.NewMockStatusPublisher(ctrl),
		invoices:     mockhandlers.NewMockUnpaidInvoices(ctrl),
		teamID:       uuid.New(),
		userID:       uuid.New(),
	}
	f.ctx = context.WithValue(context.Background(), constants.CurrentUserID, f.userID.String())
	f.handler = handlers.NewPaymentMethod(
		logger.Noop(), handlers.NewAuthorizer(f.access), f.access, mockservices.NewMockIProfileService(ctrl), f.methods, f.statuses,
		mockhandlers.NewMockProviderRegistry(ctrl), f.transactions, f.publisher, f.invoices, "https://app.example", func() time.Time { return fixedNow },
	)

	return f
}

// owner seats the caller as an owner and opens the transaction the delete
// runs in, with the Team's status row locked.
func (f *deleteFixture) owner(method *paymentmethod.PaymentMethod) {
	f.access.EXPECT().FindTeamMembership(gomock.Any(), f.teamID, f.userID).
		Return(&access.Membership{TeamID: f.teamID, UserID: f.userID, Role: authz.RoleOwner}, nil)
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.statuses.EXPECT().FindForUpdate(gomock.Any(), f.teamID, fixedNow).Return(billingstatus.Default(f.teamID, fixedNow), nil)
	f.methods.EXPECT().FindByTeamID(gomock.Any(), f.teamID).Return(paymentmethod.PaymentMethods{method}, nil)
}

func (f *deleteFixture) request(id uuid.UUID) *paymentmethod.DeleteRequest {
	return &paymentmethod.DeleteRequest{TeamID: f.teamID.String(), MethodID: id.String()}
}

func TestDelete_LastCardWhileAnInvoiceIsUnpaidIsRefused(t *testing.T) {
	f := newDeleteFixture(t)
	last := &paymentmethod.PaymentMethod{ID: uuid.New(), TeamID: f.teamID, IsDefault: true}
	f.owner(last)
	f.invoices.EXPECT().UnpaidExists(gomock.Any(), f.teamID).Return(true, nil)
	f.transactions.EXPECT().Rollback(gomock.Any()).Return(nil)

	err := f.handler.Delete(f.ctx, f.request(last.ID))

	assert.ErrorIs(t, err, paymentmethod.ErrorLastCardOwed)
}

func TestDelete_LastCardWithoutDebtBlocksTheTeamAfterCommit(t *testing.T) {
	f := newDeleteFixture(t)
	last := &paymentmethod.PaymentMethod{ID: uuid.New(), TeamID: f.teamID, IsDefault: true}
	f.owner(last)
	f.invoices.EXPECT().UnpaidExists(gomock.Any(), f.teamID).Return(false, nil)
	f.methods.EXPECT().Delete(gomock.Any(), f.teamID, last.ID, fixedNow).Return(nil)
	f.statuses.EXPECT().Transition(gomock.Any(), f.teamID, []billingstatus.Status{billingstatus.Active}, billingstatus.NoPaymentMethod, fixedNow).
		Return(&billingstatus.TeamStatus{TeamID: f.teamID, Status: billingstatus.NoPaymentMethod, ChangedAt: fixedNow}, nil)
	gomock.InOrder(
		f.transactions.EXPECT().Commit(gomock.Any()).Return(nil),
		f.publisher.EXPECT().Publish(gomock.Any(), f.teamID, "no_payment_method", fixedNow).Return(nil),
	)

	require.NoError(t, f.handler.Delete(f.ctx, f.request(last.ID)))
}

func TestDelete_NonMemberIsNotFound(t *testing.T) {
	f := newDeleteFixture(t)
	f.access.EXPECT().FindTeamMembership(gomock.Any(), f.teamID, f.userID).Return(nil, nil)

	err := f.handler.Delete(f.ctx, f.request(uuid.New()))

	assert.ErrorIs(t, err, access.ErrorTeamNotFound)
}

func TestDelete_DeveloperIsForbidden(t *testing.T) {
	f := newDeleteFixture(t)
	f.access.EXPECT().FindTeamMembership(gomock.Any(), f.teamID, f.userID).
		Return(&access.Membership{TeamID: f.teamID, UserID: f.userID, Role: authz.RoleDeveloper}, nil)

	err := f.handler.Delete(f.ctx, f.request(uuid.New()))

	assert.ErrorIs(t, err, access.ErrorForbidden)
}
