package producers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/outbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/contexts/customer/async/producers"
	mockproducers "github.com/piggystats/nodera/contexts/customer/mock/producers"
	contextConstants "github.com/piggystats/nodera/pkg/constants"
)

func TestProjectCreated_PayloadNamesTheProjectInUTC(t *testing.T) {
	client := mockproducers.NewMockOutboxClient(gomock.NewController(t))
	var sent *outbox.Message
	client.EXPECT().PublishWithOutbox(gomock.Any(), "correlation-1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, message *outbox.Message) (string, error) {
			sent = message

			return "outbox-1", nil
		})
	ctx := context.WithValue(context.Background(), contextConstants.CorrelationID, "correlation-1")
	p := &project.Project{
		ID:        uuid.New(),
		TeamID:    uuid.New(),
		Name:      "Storefront",
		CreatedAt: time.Date(2026, 10, 5, 12, 30, 0, 0, time.FixedZone("TRT", 3*60*60)),
	}

	require.NoError(t, producers.NewProjectCreated(client).Publish(ctx, p))

	require.NotNil(t, sent)
	assert.Equal(t, constants.CommonExchange, sent.ExchangeName)
	assert.Equal(t, constants.ProjectCreated, sent.RoutingKey)
	assert.JSONEq(t, fmt.Sprintf(
		`{"payload":{"project_id":%q,"team_id":%q,"name":"Storefront","created_at":"2026-10-05T09:30:00Z"}}`,
		p.ID, p.TeamID,
	), sent.Payload)
}
