package consumers_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	mockconsumers "github.com/piggystats/nodera/contexts/notification/mock/consumers"
	mockservices "github.com/piggystats/nodera/contexts/notification/mock/services"
	"github.com/piggystats/nodera/pkg/authz"
)

func TestRoleChanged_UpdatesTheReadModelBeforeTellingTheMember(t *testing.T) {
	ctrl := gomock.NewController(t)
	access, teams := mockservices.NewMockIAccessService(ctrl), mockconsumers.NewMockITeamsHandler(ctrl)
	teamID, userID, by := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	gomock.InOrder(
		access.EXPECT().UpdateTeamMemberRole(gomock.Any(), teamID, userID, authz.RoleFinance, at).Return(nil),
		teams.EXPECT().RoleChanged(gomock.Any(), handlers.RoleChange{TeamID: teamID, UserID: userID, Role: "finance", ChangedBy: by, ChangedAt: at}).Return(nil),
	)

	err := consumers.NewCustomerEvents(access, teams).Consume(message(t, constants.TeamMemberRoleChanged, map[string]any{
		"team_id": teamID, "user_id": userID, "role": "finance", "changed_at": at, "changed_by": by,
	}))

	require.NoError(t, err)
}

func TestMemberRemoved_TombstonesBeforeTellingTheMember(t *testing.T) {
	ctrl := gomock.NewController(t)
	access, teams := mockservices.NewMockIAccessService(ctrl), mockconsumers.NewMockITeamsHandler(ctrl)
	teamID, userID, by := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	gomock.InOrder(
		access.EXPECT().RemoveTeamMember(gomock.Any(), teamID, userID, at).Return(nil),
		teams.EXPECT().MemberRemoved(gomock.Any(), handlers.Removal{TeamID: teamID, UserID: userID, RemovedBy: by, RemovedAt: at}).Return(nil),
	)

	err := consumers.NewCustomerEvents(access, teams).Consume(message(t, constants.TeamMemberRemoved, map[string]any{
		"team_id": teamID, "user_id": userID, "removed_at": at, "removed_by": by,
	}))

	require.NoError(t, err)
}

func TestProfileUpdated_WritesTheNameAndLocale(t *testing.T) {
	ctrl := gomock.NewController(t)
	access := mockservices.NewMockIAccessService(ctrl)
	userID := uuid.New()
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	access.EXPECT().UpdateProfile(gomock.Any(), userID, "Zeynep Kaya", "tr", at).Return(nil)

	err := consumers.NewCustomerEvents(access, mockconsumers.NewMockITeamsHandler(ctrl)).Consume(message(t, constants.UserProfileUpdated, map[string]any{
		"user_id": userID, "full_name": "Zeynep Kaya", "locale": "tr", "updated_at": at,
	}))

	require.NoError(t, err)
}

func TestProfileUpdated_WithoutItsTimeIsRefused(t *testing.T) {
	ctrl := gomock.NewController(t)

	err := consumers.NewCustomerEvents(mockservices.NewMockIAccessService(ctrl), mockconsumers.NewMockITeamsHandler(ctrl)).
		Consume(message(t, constants.UserProfileUpdated, map[string]any{"user_id": uuid.New(), "locale": "tr"}))

	require.Error(t, err)
}
