package usage

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// UpsertSize keeps the day's last measurement.
func (r *Repository) UpsertSize(ctx context.Context, s *Size) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO storage_sizes (storage_id, day, used_bytes, object_count)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (storage_id, day) DO UPDATE SET used_bytes = EXCLUDED.used_bytes, object_count = EXCLUDED.object_count`,
		s.StorageID, s.Day, s.UsedBytes, s.ObjectCount)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// UpsertTraffic replaces an hour read again, so a usage log that was still
// filling when first read is corrected, never doubled.
func (r *Repository) UpsertTraffic(ctx context.Context, hours []*Traffic) error {
	for _, h := range hours {
		_, err := r.queries.Executor(ctx).Exec(ctx, `
			INSERT INTO storage_traffic (storage_id, hour, bytes_sent, bytes_received, requests)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (storage_id, hour) DO UPDATE
			SET bytes_sent = EXCLUDED.bytes_sent, bytes_received = EXCLUDED.bytes_received, requests = EXCLUDED.requests`,
			h.StorageID, h.Hour, h.BytesSent, h.BytesReceived, h.Requests)
		if err != nil {
			return ErrorCannotStore.Copy().SetError(err)
		}
	}

	return nil
}

// ListDays answers the days in [from, to) that saw a measurement or any
// traffic, oldest first.
func (r *Repository) ListDays(ctx context.Context, storageID uuid.UUID, from, to time.Time) ([]*Day, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		WITH traffic AS (
			SELECT (hour AT TIME ZONE 'UTC')::date AS day, sum(bytes_sent) AS sent, sum(bytes_received) AS received, sum(requests) AS requests
			FROM storage_traffic WHERE storage_id = $1 AND hour >= $2 AND hour < $3 GROUP BY 1
		), sizes AS (
			SELECT day, used_bytes, object_count FROM storage_sizes WHERE storage_id = $1 AND day >= $2::date AND day < $3::date
		)
		SELECT COALESCE(sizes.day, traffic.day), COALESCE(sizes.used_bytes, 0), COALESCE(sizes.object_count, 0),
			COALESCE(traffic.sent, 0), COALESCE(traffic.received, 0), COALESCE(traffic.requests, 0)
		FROM sizes FULL OUTER JOIN traffic ON traffic.day = sizes.day
		ORDER BY 1`, storageID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Day{}
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.Day, &d.UsedBytes, &d.ObjectCount, &d.BytesSent, &d.BytesReceived, &d.Requests); err != nil {
			return nil, err
		}
		d.Day = DayOf(d.Day)
		out = append(out, &d)
	}

	return out, rows.Err()
}

func (r *Repository) DeleteByStorage(ctx context.Context, storageID uuid.UUID) error {
	for _, table := range []string{"storage_traffic", "storage_sizes"} {
		if _, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM `+table+` WHERE storage_id = $1`, storageID); err != nil {
			return ErrorCannotStore.Copy().SetError(err)
		}
	}

	return nil
}
