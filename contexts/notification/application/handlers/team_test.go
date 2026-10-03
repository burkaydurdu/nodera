package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/access"
	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	mockhandlers "github.com/piggystats/nodera/contexts/notification/mock/handlers"
	mockservices "github.com/piggystats/nodera/contexts/notification/mock/services"
)

func TestRoleChanged_BySomeoneElseIsToldInsideTheTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	teams, notifier := mockservices.NewMockIAccessService(ctrl), mockhandlers.NewMockIPersonNotifier(ctrl)
	teamID, userID := uuid.New(), uuid.New()
	teams.EXPECT().FindTeam(gomock.Any(), teamID).Return(&access.Team{TeamID: teamID, Name: "Acme"}, nil)
	var told handlers.PersonEvent
	notifier.EXPECT().NotifyPerson(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e handlers.PersonEvent) error {
		told = e

		return nil
	})

	err := handlers.NewTeams(teams, notifier).RoleChanged(context.Background(), handlers.RoleChange{
		TeamID: teamID, UserID: userID, Role: "finance", ChangedBy: uuid.New(), ChangedAt: fixedNow,
	})

	require.NoError(t, err)
	assert.Equal(t, notification.TypeTeamRoleChanged, told.Type)
	assert.Equal(t, &userID, told.Recipient.UserID)
	assert.Equal(t, &teamID, told.TeamID)
	assert.Equal(t, map[string]any{"team_id": teamID.String(), "team_name": "Acme", "role": "finance"}, told.Params)
}

func TestRoleChanged_ByTheMemberThemselvesIsNotTold(t *testing.T) {
	ctrl := gomock.NewController(t)
	userID := uuid.New()

	err := handlers.NewTeams(mockservices.NewMockIAccessService(ctrl), mockhandlers.NewMockIPersonNotifier(ctrl)).RoleChanged(context.Background(),
		handlers.RoleChange{TeamID: uuid.New(), UserID: userID, Role: "developer", ChangedBy: userID, ChangedAt: fixedNow})

	require.NoError(t, err)
}

func TestMemberRemoved_BelongsToNoTeamSoItStaysVisible(t *testing.T) {
	ctrl := gomock.NewController(t)
	teams, notifier := mockservices.NewMockIAccessService(ctrl), mockhandlers.NewMockIPersonNotifier(ctrl)
	teamID := uuid.New()
	teams.EXPECT().FindTeam(gomock.Any(), teamID).Return(&access.Team{TeamID: teamID, Name: "Acme"}, nil)
	var told handlers.PersonEvent
	notifier.EXPECT().NotifyPerson(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e handlers.PersonEvent) error {
		told = e

		return nil
	})

	err := handlers.NewTeams(teams, notifier).MemberRemoved(context.Background(), handlers.Removal{
		TeamID: teamID, UserID: uuid.New(), RemovedBy: uuid.New(), RemovedAt: time.Now(),
	})

	require.NoError(t, err)
	assert.Nil(t, told.TeamID)
	assert.Equal(t, "Acme", told.Params["team_name"])
}

func TestMemberRemoved_LeavingIsNotTold(t *testing.T) {
	ctrl := gomock.NewController(t)
	userID := uuid.New()

	err := handlers.NewTeams(mockservices.NewMockIAccessService(ctrl), mockhandlers.NewMockIPersonNotifier(ctrl)).MemberRemoved(context.Background(),
		handlers.Removal{TeamID: uuid.New(), UserID: userID, RemovedBy: userID, RemovedAt: time.Now()})

	require.NoError(t, err)
}
