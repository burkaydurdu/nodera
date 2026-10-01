package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/application/handlers"
	"github.com/piggystats/nodera/contexts/customer/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/constants"
)

// ReplayProjects writes TeamCreated, TeamMemberAdded and ProjectCreated for
// every existing row into the outbox. The binary's `replay-projects` command
// and the integration suite both go through it. One correlation id ties the
// whole replay together downstream, since no request started it.
func ReplayProjects(ctx context.Context, log logger.ILog, pool *pgxpool.Pool, outboxClient producers.OutboxClient) (*handlers.ReplaySummary, error) {
	ctx = context.WithValue(ctx, constants.CorrelationID, uuid.NewString())

	queries := postgresql.New(pool)
	replay := handlers.NewReplay(
		team.NewService(team.NewRepository(queries)),
		project.NewService(project.NewRepository(queries)),
		producers.NewTeamCreated(outboxClient),
		producers.NewTeamMemberAdded(outboxClient),
		producers.NewProjectCreated(outboxClient),
	)

	summary, err := replay.Run(ctx)
	if err != nil {
		return nil, err
	}

	log.Infof("replay-projects queued %d TeamCreated, %d TeamMemberAdded and %d ProjectCreated events", summary.Teams, summary.Members, summary.Projects)

	return summary, nil
}
