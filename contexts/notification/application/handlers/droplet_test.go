package handlers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	mockhandlers "github.com/piggystats/nodera/contexts/notification/mock/handlers"
)

func change(from, to string) handlers.DropletStatusChange {
	return handlers.DropletStatusChange{DropletID: uuid.New(), TeamID: uuid.New(), ProjectID: uuid.New(), Hostname: "web-1", IPv4: "203.0.113.7", From: &from, Status: to}
}

func TestStatusChanged_ADropletLeavingProvisioningIsReady(t *testing.T) {
	notifier := mockhandlers.NewMockITeamNotifier(gomock.NewController(t))
	c := change("provisioning", "active")
	notifier.EXPECT().NotifyTeam(gomock.Any(), handlers.TeamEvent{
		SourceKey: "droplet_active:" + c.DropletID.String(), Type: notification.TypeDropletActive, TeamID: c.TeamID,
		Params: map[string]any{"droplet_id": c.DropletID.String(), "project_id": c.ProjectID.String(), "hostname": "web-1", "ipv4": "203.0.113.7"},
	}).Return(nil)

	require.NoError(t, handlers.NewDroplets(notifier).StatusChanged(context.Background(), c))
}

func TestStatusChanged_PoweringBackOnIsNoNews(t *testing.T) {
	notifier := mockhandlers.NewMockITeamNotifier(gomock.NewController(t))

	require.NoError(t, handlers.NewDroplets(notifier).StatusChanged(context.Background(), change("off", "active")))
}

func TestStatusChanged_AFailureWithoutACodeIsWordedAsUnavailable(t *testing.T) {
	notifier := mockhandlers.NewMockITeamNotifier(gomock.NewController(t))
	var told handlers.TeamEvent
	notifier.EXPECT().NotifyTeam(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e handlers.TeamEvent) error {
		told = e

		return nil
	})

	require.NoError(t, handlers.NewDroplets(notifier).StatusChanged(context.Background(), change("provisioning", "failed")))

	assert.Equal(t, notification.TypeDropletFailed, told.Type)
	assert.Equal(t, "unavailable", told.Params["failure_code"])
	assert.Equal(t, "droplet_failure_unavailable", told.EmailParams["FailureKey"])
}
