package handlers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/access"
	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/email"
	mockhandlers "github.com/piggystats/nodera/contexts/notification/mock/handlers"
	mockservices "github.com/piggystats/nodera/contexts/notification/mock/services"
	"github.com/piggystats/nodera/logger"
	mockpersistence "github.com/piggystats/nodera/mock/persistence"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

var fixedNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

type dispatchFixture struct {
	notifications *mockservices.MockINotificationService
	access        *mockservices.MockIAccessService
	transactions  *mockpersistence.MockITransactionManager
	email         *mockhandlers.MockIEmailSender
	realtime      *mockhandlers.MockIRealtimePublisher
	dispatcher    *handlers.Dispatcher
	teamID        uuid.UUID
	recorded      []*notification.Notification
	channels      map[string][]notification.Channel
}

func newDispatchFixture(t *testing.T) *dispatchFixture {
	ctrl := gomock.NewController(t)
	f := &dispatchFixture{
		notifications: mockservices.NewMockINotificationService(ctrl),
		access:        mockservices.NewMockIAccessService(ctrl),
		transactions:  mockpersistence.NewMockITransactionManager(ctrl),
		email:         mockhandlers.NewMockIEmailSender(ctrl),
		realtime:      mockhandlers.NewMockIRealtimePublisher(ctrl),
		teamID:        uuid.New(),
		channels:      map[string][]notification.Channel{},
	}
	f.dispatcher = handlers.NewDispatcher(logger.Noop(), f.notifications, f.access, f.transactions, f.email, f.realtime, func() time.Time { return fixedNow })

	return f
}

// team seats the members and records whatever the dispatcher records, with
// nothing left to deliver afterwards.
func (f *dispatchFixture) team(members ...access.Member) {
	f.access.EXPECT().MembersOf(gomock.Any(), f.teamID).Return(members, nil)
	f.access.EXPECT().FindTeam(gomock.Any(), f.teamID).Return(&access.Team{TeamID: f.teamID, Name: "Acme"}, nil)
	f.expectRecording()
	f.notifications.EXPECT().FindPending(gomock.Any(), gomock.Any()).Return(nil, nil)
}

func (f *dispatchFixture) expectRecording() {
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.notifications.EXPECT().Record(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, n *notification.Notification, channels []notification.Channel) (bool, error) {
			f.recorded = append(f.recorded, n)
			f.channels[n.Recipient.Key()] = channels

			return true, nil
		})
	f.transactions.EXPECT().Commit(gomock.Any()).Return(nil)
}

func member(role authz.Role, address string) access.Member {
	return access.Member{UserID: uuid.New(), Role: role, Email: address, Locale: "en"}
}

func invoiceEvent(teamID uuid.UUID, also ...notification.Recipient) handlers.TeamEvent {
	return handlers.TeamEvent{SourceKey: "invoice_issued:1", Type: notification.TypeInvoiceIssued, TeamID: teamID, Params: map[string]any{}, Also: also}
}

func TestNotifyTeam_TellsOnlyTheMembersWhoseRoleHoldsThePermission(t *testing.T) {
	f := newDispatchFixture(t)
	owner, finance, developer := member(authz.RoleOwner, "o@x"), member(authz.RoleFinance, "f@x"), member(authz.RoleDeveloper, "d@x")
	f.team(owner, finance, developer)

	require.NoError(t, f.dispatcher.NotifyTeam(context.Background(), invoiceEvent(f.teamID)))

	require.Len(t, f.recorded, 2)
	assert.Equal(t, &owner.UserID, f.recorded[0].Recipient.UserID)
	assert.Equal(t, &finance.UserID, f.recorded[1].Recipient.UserID, "billing.read, not the developer")
	assert.True(t, f.recorded[0].InInbox)
	assert.Equal(t, &f.teamID, f.recorded[0].TeamID)
}

func TestNotifyTeam_TellsTheInvoiceAddressByEmailUnlessAMemberHoldsIt(t *testing.T) {
	f := newDispatchFixture(t)
	owner := member(authz.RoleOwner, "Books@Acme.example")
	f.team(owner)

	require.NoError(t, f.dispatcher.NotifyTeam(context.Background(), invoiceEvent(f.teamID,
		notification.Recipient{Email: "books@acme.example"}, notification.Recipient{Email: "accounts@acme.example", Locale: "tr"},
	)))

	require.Len(t, f.recorded, 2, "the owner's address is told once")
	address := f.recorded[1]
	assert.Nil(t, address.Recipient.UserID)
	assert.False(t, address.InInbox, "an address has no Inbox")
	assert.Equal(t, []notification.Channel{notification.ChannelEmail}, f.channels[address.Recipient.Key()])
}

func TestNotifyTeam_AMemberWhoseEmailNeverArrivedIsToldInTheAppOnly(t *testing.T) {
	f := newDispatchFixture(t)
	owner := member(authz.RoleOwner, "")
	f.team(owner)

	require.NoError(t, f.dispatcher.NotifyTeam(context.Background(), invoiceEvent(f.teamID)))

	assert.Equal(t, []notification.Channel{notification.ChannelInApp}, f.channels["user:"+owner.UserID.String()])
}

func TestNotifyTeam_AnUnknownTypeIsRefused(t *testing.T) {
	f := newDispatchFixture(t)

	err := f.dispatcher.NotifyTeam(context.Background(), handlers.TeamEvent{Type: "unknown", TeamID: f.teamID})

	require.Error(t, err)
}

func pendingFor(userID uuid.UUID, channels ...notification.Channel) []notification.Pending {
	return []notification.Pending{{
		Notification: &notification.Notification{
			ID: uuid.New(), Type: notification.TypeDropletFailed, CreatedAt: fixedNow,
			Recipient: notification.Recipient{UserID: &userID, Email: "ada@example.com", FullName: "Ada", Locale: "tr"},
			Params:    map[string]any{"hostname": "web-1"},
		},
		Channels: channels,
	}}
}

func (f *dispatchFixture) personEvent(userID uuid.UUID) handlers.PersonEvent {
	return handlers.PersonEvent{
		SourceKey: "droplet_failed:1", Type: notification.TypeDropletFailed,
		Recipient: notification.Recipient{UserID: &userID, Email: "ada@example.com"}, EmailParams: map[string]any{"Hostname": "web-1"},
	}
}

func TestDeliver_TheEmailSeesTheRecipientAndTheEventsEmailParams(t *testing.T) {
	f := newDispatchFixture(t)
	userID := uuid.New()
	f.expectRecording()
	pending := pendingFor(userID, notification.ChannelEmail)
	f.notifications.EXPECT().FindPending(gomock.Any(), "droplet_failed:1").Return(pending, nil)
	f.email.EXPECT().Send(gomock.Any(), email.KindDropletFailed, "ada@example.com", "tr", map[string]any{
		"FullName": "Ada", "Email": "ada@example.com", "Hostname": "web-1",
	}).Return(nil)
	f.notifications.EXPECT().MarkDelivered(gomock.Any(), pending[0].Notification.ID, notification.ChannelEmail, fixedNow).Return(nil)

	require.NoError(t, f.dispatcher.NotifyPerson(context.Background(), f.personEvent(userID)))
}

func TestDeliver_TheInAppPushCarriesTheInboxItemToItsUser(t *testing.T) {
	f := newDispatchFixture(t)
	userID := uuid.New()
	f.expectRecording()
	pending := pendingFor(userID, notification.ChannelInApp)
	f.notifications.EXPECT().FindPending(gomock.Any(), gomock.Any()).Return(pending, nil)
	f.realtime.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *realtime.Message) error {
		assert.Equal(t, realtime.ToUser(userID), m.Payload.Address)
		assert.Equal(t, handlers.LiveNotificationCreated, m.Payload.Type)
		assert.Contains(t, string(m.Payload.Data), pending[0].Notification.ID.String())

		return nil
	})
	f.notifications.EXPECT().MarkDelivered(gomock.Any(), pending[0].Notification.ID, notification.ChannelInApp, fixedNow).Return(nil)

	require.NoError(t, f.dispatcher.NotifyPerson(context.Background(), f.personEvent(userID)))
}

func TestDeliver_AFailedChannelIsMarkedAndReportedWhileTheOthersGoOut(t *testing.T) {
	f := newDispatchFixture(t)
	userID := uuid.New()
	f.expectRecording()
	pending := pendingFor(userID, notification.ChannelEmail, notification.ChannelInApp)
	f.notifications.EXPECT().FindPending(gomock.Any(), gomock.Any()).Return(pending, nil)
	unavailable := errors.New("smtp unavailable")
	f.email.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(unavailable)
	f.notifications.EXPECT().MarkFailed(gomock.Any(), pending[0].Notification.ID, notification.ChannelEmail, "smtp unavailable").Return(nil)
	f.realtime.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
	f.notifications.EXPECT().MarkDelivered(gomock.Any(), pending[0].Notification.ID, notification.ChannelInApp, fixedNow).Return(nil)

	err := f.dispatcher.NotifyPerson(context.Background(), f.personEvent(userID))

	require.ErrorIs(t, err, unavailable, "the event goes to the error queue and its redelivery retries the email")
}
