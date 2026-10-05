package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
)

// periods carries a Project into the Period holding a given instant. The
// rollover job does it for every Project; a Message or an overview at the very
// start of a month does it for its own Project first, so nobody depends on
// the job having run.
type periods struct {
	access    access.IService
	packages  packages.IService
	announcer Announcer
}

// ensure runs inside the caller's transaction. A change scheduled for the new
// Period applies at its first instant, not when the job happens to run, so
// billing's intervals meet exactly at the boundary.
func (p *periods) ensure(ctx context.Context, projectID uuid.UUID, now time.Time, out *announcements) (*packages.ProjectPackage, *packages.Period, error) {
	pp, err := p.packages.FindProjectForUpdate(ctx, projectID)
	if err != nil || pp == nil {
		return pp, nil, err
	}

	start := packages.PeriodStart(now)
	period, err := p.packages.FindPeriod(ctx, projectID, start)
	if err != nil || period != nil {
		return pp, period, err
	}

	if pp.Scheduled != nil && pp.Current != nil && pp.Current.StartedAt.Before(start) {
		if err := p.applySchedule(ctx, pp, start, out); err != nil {
			return nil, nil, err
		}
	}
	if pp.Current == nil {
		return pp, nil, nil
	}

	period = &packages.Period{ProjectID: projectID, Start: start, Allowance: pp.Current.MonthlyEmails}
	if err := p.packages.UpsertPeriod(ctx, period); err != nil {
		return nil, nil, err
	}

	return pp, period, nil
}

func (p *periods) applySchedule(ctx context.Context, pp *packages.ProjectPackage, start time.Time, out *announcements) error {
	project, err := p.access.FindProject(ctx, pp.ProjectID)
	if err != nil {
		return err
	}

	old := pp.Current
	if pp.Scheduled.Cancels() {
		pp.Current = nil
	} else {
		pp.Current = pp.Scheduled.HoldFrom(start)
	}
	pp.Scheduled = nil
	if err := p.packages.SaveProject(ctx, pp); err != nil {
		return err
	}

	out.add(func(ctx context.Context) error {
		return p.announcer.PackageStopped(ctx, pp.TeamID, pp.ProjectID, old.StartedAt, start)
	})
	if pp.Current != nil {
		held := *pp.Current
		out.add(func(ctx context.Context) error { return p.announcer.PackageStarted(ctx, project, &held) })
	}
	out.add(func(ctx context.Context) error {
		return p.announcer.SendingChanged(ctx, pp.TeamID, pp.ProjectID, start)
	})

	return nil
}
