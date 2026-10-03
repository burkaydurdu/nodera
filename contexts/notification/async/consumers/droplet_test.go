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
)

func TestDropletStatusChanged_HandsTheChangeOn(t *testing.T) {
	handler := mockconsumers.NewMockIDropletHandler(gomock.NewController(t))
	dropletID, teamID, projectID := uuid.New(), uuid.New(), uuid.New()
	from := "provisioning"
	handler.EXPECT().StatusChanged(gomock.Any(), handlers.DropletStatusChange{
		DropletID: dropletID, TeamID: teamID, ProjectID: projectID, Hostname: "web-1", IPv4: "203.0.113.7",
		From: &from, Status: "failed", FailureCode: "timed_out",
	}).Return(nil)

	err := consumers.NewDropletEvents(handler).Consume(message(t, constants.DropletStatusChanged, map[string]any{
		"droplet_id": dropletID, "team_id": teamID, "project_id": projectID, "hostname": "web-1", "ipv4": "203.0.113.7",
		"from_status": from, "status": "failed", "failure_code": "timed_out", "changed_at": time.Now().UTC(),
	}))

	require.NoError(t, err)
}

func TestDropletStatusChanged_WithoutItsTeamIsRefused(t *testing.T) {
	handler := mockconsumers.NewMockIDropletHandler(gomock.NewController(t))

	err := consumers.NewDropletEvents(handler).Consume(message(t, constants.DropletStatusChanged, map[string]any{
		"droplet_id": uuid.New(), "project_id": uuid.New(), "status": "active", "changed_at": time.Now().UTC(),
	}))

	require.Error(t, err)
}
