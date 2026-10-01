// Package contextutil reads the request invariants the auth middleware
// guarantees. A missing value is broken wiring, so the reads fail loud.
package contextutil

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/constants"
)

func CurrentUserID(ctx context.Context) uuid.UUID {
	raw, _ := ctx.Value(constants.CurrentUserID).(string)

	return uuid.MustParse(raw)
}

func CurrentSessionID(ctx context.Context) uuid.UUID {
	raw, _ := ctx.Value(constants.CurrentSessionID).(string)

	return uuid.MustParse(raw)
}

// ClientIP and ClientUserAgent are genuinely optional: a request that reaches
// the service without the ClientInfo middleware simply describes no device.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(constants.ClientIP).(string)

	return ip
}

func ClientUserAgent(ctx context.Context) string {
	ua, _ := ctx.Value(constants.ClientUserAgent).(string)

	return ua
}
