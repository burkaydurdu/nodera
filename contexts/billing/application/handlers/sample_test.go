package handlers_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
	"github.com/piggystats/nodera/contexts/billing/application/handlers"
	mockhandlers "github.com/piggystats/nodera/contexts/billing/mock/handlers"
	mockservices "github.com/piggystats/nodera/contexts/billing/mock/services"
	"github.com/piggystats/nodera/logger"
	mockpersistence "github.com/piggystats/nodera/mock/persistence"
)

type sampleFixture struct {
	access       *mockservices.MockIAccessService
	profiles     *mockservices.MockIProfileService
	methods      *mockservices.MockIPaymentMethodService
	statuses     *mockservices.MockIBillingStatusService
	transactions *mockpersistence.MockITransactionManager
	publisher    *mockhandlers.MockStatusPublisher
	handler      *handlers.SamplePaymentMethod
	teamID       uuid.UUID
}

func newSampleFixture(t *testing.T) *sampleFixture {
	ctrl := gomock.NewController(t)
	f := &sampleFixture{
		access:       mockservices.NewMockIAccessService(ctrl),
		profiles:     mockservices.NewMockIProfileService(ctrl),
		methods:      mockservices.NewMockIPaymentMethodService(ctrl),
		statuses:     mockservices.NewMockIBillingStatusService(ctrl),
		transactions: mockpersistence.NewMockITransactionManager(ctrl),
		publisher:    mockhandlers.NewMockStatusPublisher(ctrl),
		teamID:       uuid.New(),
	}
	f.handler = handlers.NewSamplePaymentMethod(
		logger.Noop(), f.access, f.profiles, f.methods, f.statuses, f.transactions, f.publisher, func() time.Time { return fixedNow },
	)

	return f
}

// routed finds the Team with a Turkish Billing profile and opens the
// transaction the sample is saved in, with the Team's status row locked.
func (f *sampleFixture) routed(status billingstatus.Status) {
	f.access.EXPECT().FindTeam(gomock.Any(), f.teamID).Return(&access.Team{TeamID: f.teamID, Name: "Acme"}, nil)
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.statuses.EXPECT().FindForUpdate(gomock.Any(), f.teamID, fixedNow).
		Return(&billingstatus.TeamStatus{TeamID: f.teamID, Status: status, ChangedAt: fixedNow}, nil)
	f.profiles.EXPECT().FindByTeamID(gomock.Any(), f.teamID).Return(&profile.Profile{TeamID: f.teamID, Country: "TR"}, nil)
	f.methods.EXPECT().FindRoute(gomock.Any(), "TR").Return(&paymentmethod.Route{Country: "TR", Provider: "craftgate", Currency: "TRY"}, nil)
}

func TestSamplePaymentMethod_TheFirstIsTheDefaultAndMakesTheTeamActiveAfterCommit(t *testing.T) {
	f := newSampleFixture(t)
	f.routed(billingstatus.NoPaymentMethod)
	f.methods.EXPECT().CountByTeamID(gomock.Any(), f.teamID).Return(0, nil)
	var saved *paymentmethod.PaymentMethod
	f.methods.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *paymentmethod.PaymentMethod) error {
		saved = m

		return nil
	})
	activated := &billingstatus.TeamStatus{TeamID: f.teamID, Status: billingstatus.Active, ChangedAt: fixedNow}
	f.statuses.EXPECT().Transition(gomock.Any(), f.teamID, []billingstatus.Status{billingstatus.NoPaymentMethod}, billingstatus.Active, fixedNow).
		Return(activated, nil)
	gomock.InOrder(
		f.transactions.EXPECT().Commit(gomock.Any()).Return(nil),
		f.publisher.EXPECT().Publish(gomock.Any(), f.teamID, string(billingstatus.Active), fixedNow).Return(nil),
	)

	m, status, err := f.handler.Run(context.Background(), f.teamID)

	require.NoError(t, err)
	assert.Equal(t, billingstatus.Active, status)
	require.NotNil(t, saved)
	assert.Same(t, saved, m)
	assert.True(t, m.IsDefault)
	assert.Equal(t, "craftgate", m.Provider, "routed by the Billing profile's country, like a verified one")
	assert.Equal(t, "TRY", m.Currency)
	assert.Equal(t, "4242", m.Last4)
	assert.True(t, strings.HasPrefix(m.ProviderCardRef, "sample_"), "a provider never mistakes it for its own: %q", m.ProviderCardRef)
	expiresAt, known := m.ExpiresAt()
	assert.True(t, known, "a known expiry keeps the expiry check from asking a provider")
	assert.True(t, expiresAt.After(fixedNow.AddDate(1, 0, 0)))
}

func TestSamplePaymentMethod_AnotherIsNotTheDefaultAndPublishesNothing(t *testing.T) {
	f := newSampleFixture(t)
	f.routed(billingstatus.Active)
	f.methods.EXPECT().CountByTeamID(gomock.Any(), f.teamID).Return(1, nil)
	f.methods.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.statuses.EXPECT().Transition(gomock.Any(), f.teamID, []billingstatus.Status{billingstatus.NoPaymentMethod}, billingstatus.Active, fixedNow).
		Return(nil, nil)
	f.transactions.EXPECT().Commit(gomock.Any()).Return(nil)

	m, status, err := f.handler.Run(context.Background(), f.teamID)

	require.NoError(t, err)
	assert.False(t, m.IsDefault)
	assert.Equal(t, billingstatus.Active, status)
}

func TestSamplePaymentMethod_ASuspendedTeamStaysSuspended(t *testing.T) {
	f := newSampleFixture(t)
	f.routed(billingstatus.Suspended)
	f.methods.EXPECT().CountByTeamID(gomock.Any(), f.teamID).Return(1, nil)
	f.methods.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.statuses.EXPECT().Transition(gomock.Any(), f.teamID, []billingstatus.Status{billingstatus.NoPaymentMethod}, billingstatus.Active, fixedNow).
		Return(nil, nil)
	f.transactions.EXPECT().Commit(gomock.Any()).Return(nil)

	_, status, err := f.handler.Run(context.Background(), f.teamID)

	require.NoError(t, err)
	assert.Equal(t, billingstatus.Suspended, status, "only paying the debt lifts a suspension")
}

func TestSamplePaymentMethod_ATeamWithoutABillingProfileIsRefused(t *testing.T) {
	f := newSampleFixture(t)
	f.access.EXPECT().FindTeam(gomock.Any(), f.teamID).Return(&access.Team{TeamID: f.teamID}, nil)
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.statuses.EXPECT().FindForUpdate(gomock.Any(), f.teamID, fixedNow).Return(billingstatus.Default(f.teamID, fixedNow), nil)
	f.profiles.EXPECT().FindByTeamID(gomock.Any(), f.teamID).Return(nil, profile.ErrorNotFound)
	f.transactions.EXPECT().Rollback(gomock.Any()).Return(nil)

	_, _, err := f.handler.Run(context.Background(), f.teamID)

	assert.ErrorIs(t, err, paymentmethod.ErrorProfileRequired)
}

func TestSamplePaymentMethod_AnUnknownTeamIsNotFound(t *testing.T) {
	f := newSampleFixture(t)
	f.access.EXPECT().FindTeam(gomock.Any(), f.teamID).Return(nil, nil)

	_, _, err := f.handler.Run(context.Background(), f.teamID)

	assert.ErrorIs(t, err, access.ErrorTeamNotFound)
}
