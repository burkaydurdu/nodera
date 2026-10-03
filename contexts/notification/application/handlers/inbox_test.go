package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	mockhandlers "github.com/piggystats/nodera/contexts/notification/mock/handlers"
	mockservices "github.com/piggystats/nodera/contexts/notification/mock/services"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/pkg/realtime"
)

type inboxFixture struct {
	notifications *mockservices.MockINotificationService
	realtime      *mockhandlers.MockIRealtimePublisher
	inbox         *handlers.Inbox
	ctx           context.Context
	userID        uuid.UUID
	teams         []uuid.UUID
}

// newInboxFixture signs a caller in whose Teams are known to the read model.
func newInboxFixture(t *testing.T) *inboxFixture {
	ctrl := gomock.NewController(t)
	accessService := mockservices.NewMockIAccessService(ctrl)
	f := &inboxFixture{
		notifications: mockservices.NewMockINotificationService(ctrl),
		realtime:      mockhandlers.NewMockIRealtimePublisher(ctrl),
		userID:        uuid.New(),
		teams:         []uuid.UUID{uuid.New()},
	}
	f.ctx = context.WithValue(context.Background(), constants.CurrentUserID, f.userID.String())
	f.inbox = handlers.NewInbox(logger.Noop(), f.notifications, accessService, f.realtime, func() time.Time { return fixedNow })
	accessService.EXPECT().TeamsOf(gomock.Any(), f.userID).Return(f.teams, nil)

	return f
}

// published captures the realtime message the handler sends.
func (f *inboxFixture) published() *realtime.Body {
	var body realtime.Body
	f.realtime.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *realtime.Message) error {
		body = m.Payload

		return nil
	})

	return &body
}

func TestList_ShowsOnlyTheTeamsTheCallerIsStillIn(t *testing.T) {
	f := newInboxFixture(t)
	before := uuid.New()
	f.notifications.EXPECT().ListInbox(gomock.Any(), f.userID, f.teams, &before, 5).Return([]*notification.Notification{}, nil)

	items, err := f.inbox.List(f.ctx, &notification.ListRequest{Before: before.String(), Limit: 5})

	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestRead_TellsTheOtherTabsWhichOne(t *testing.T) {
	f := newInboxFixture(t)
	id := uuid.New()
	f.notifications.EXPECT().MarkRead(gomock.Any(), f.userID, f.teams, id, fixedNow).
		Return(&notification.Notification{ID: id, ReadAt: &fixedNow}, nil)
	told := f.published()

	read, err := f.inbox.Read(f.ctx, &notification.ReadRequest{NotificationID: id.String()})

	require.NoError(t, err)
	assert.Equal(t, &fixedNow, read.ReadAt)
	assert.Equal(t, realtime.ToUser(f.userID), told.Address)
	assert.Equal(t, handlers.LiveNotificationRead, told.Type)
	assert.JSONEq(t, `{"notification_ids":["`+id.String()+`"]}`, string(told.Data))
}

func TestReadAll_TellsTheOtherTabsThatEverythingIsRead(t *testing.T) {
	f := newInboxFixture(t)
	f.notifications.EXPECT().MarkAllRead(gomock.Any(), f.userID, f.teams, fixedNow).Return([]uuid.UUID{uuid.New(), uuid.New()}, nil)
	told := f.published()

	count, err := f.inbox.ReadAll(f.ctx)

	require.NoError(t, err)
	assert.Equal(t, 2, count.Count)
	assert.JSONEq(t, `{"all":true}`, string(told.Data))
}

func TestReadAll_WithNothingUnreadTellsNobody(t *testing.T) {
	f := newInboxFixture(t)
	f.notifications.EXPECT().MarkAllRead(gomock.Any(), f.userID, f.teams, fixedNow).Return([]uuid.UUID{}, nil)

	count, err := f.inbox.ReadAll(f.ctx)

	require.NoError(t, err)
	assert.Equal(t, 0, count.Count)
}
