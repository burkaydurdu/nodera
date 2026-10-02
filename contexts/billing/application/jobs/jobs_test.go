package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/application/jobs"
	mockjobs "github.com/piggystats/nodera/contexts/billing/mock/jobs"
	"github.com/piggystats/nodera/logger"
)

func TestClosePeriods_ClosesThePeriodThatJustEnded(t *testing.T) {
	ctrl := gomock.NewController(t)
	collection := mockjobs.NewMockCollection(ctrl)
	now := time.Date(2026, 11, 1, 0, 5, 0, 0, time.UTC)
	collection.EXPECT().ClosePeriod(gomock.Any(), usage.Period{
		Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}, now).Return(nil)

	err := jobs.New(logger.Noop(), nil, nil, collection, func() time.Time { return now }).ClosePeriods(context.Background())

	require.NoError(t, err)
}

func TestClosePeriods_InJanuaryClosesDecember(t *testing.T) {
	ctrl := gomock.NewController(t)
	collection := mockjobs.NewMockCollection(ctrl)
	local := time.FixedZone("UTC+3", 3*60*60)
	now := time.Date(2027, 1, 3, 2, 0, 0, 0, local)
	collection.EXPECT().ClosePeriod(gomock.Any(), usage.Period{
		Start: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}, now.UTC()).Return(nil)

	require.NoError(t, jobs.New(logger.Noop(), nil, nil, collection, func() time.Time { return now }).ClosePeriods(context.Background()))
}

func TestCollectionJobs_RunAtTheClocksInstantInUTC(t *testing.T) {
	ctrl := gomock.NewController(t)
	collection := mockjobs.NewMockCollection(ctrl)
	now := time.Date(2026, 10, 5, 7, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	collection.EXPECT().CheckThresholds(gomock.Any(), now.UTC()).Return(nil)
	collection.EXPECT().RunDunning(gomock.Any(), now.UTC()).Return(nil)
	collection.EXPECT().Reconcile(gomock.Any(), now.UTC()).Return(nil)
	collection.EXPECT().Sweep(gomock.Any(), now.UTC()).Return(nil)
	collection.EXPECT().WarnExpiringCards(gomock.Any(), now.UTC()).Return(nil)
	j := jobs.New(logger.Noop(), nil, nil, collection, func() time.Time { return now })

	require.NoError(t, j.CheckThresholds(context.Background()))
	require.NoError(t, j.RunDunning(context.Background()))
	require.NoError(t, j.ReconcilePayments(context.Background()))
	require.NoError(t, j.SweepInvoices(context.Background()))
	require.NoError(t, j.CardExpiring(context.Background()))
}
