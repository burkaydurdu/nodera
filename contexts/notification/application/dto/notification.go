package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
)

// Notification is an Inbox item; the web app writes its text from the type
// and the params, in the reader's language.
type Notification struct {
	ID        uuid.UUID      `json:"id"`
	Type      string         `json:"type" enums:"droplet_active,droplet_failed"`
	TeamID    *uuid.UUID     `json:"team_id"`
	Params    map[string]any `json:"params"`
	ReadAt    *time.Time     `json:"read_at"`
	CreatedAt time.Time      `json:"created_at"`
}

func ToNotification(n *notification.Notification) *Notification {
	params := n.Params
	if params == nil {
		params = map[string]any{}
	}

	return &Notification{ID: n.ID, Type: string(n.Type), TeamID: n.TeamID, Params: params, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt}
}

func ToNotifications(ns []*notification.Notification) []*Notification {
	out := make([]*Notification, 0, len(ns))
	for _, n := range ns {
		out = append(out, ToNotification(n))
	}

	return out
}

type UnreadCount struct {
	Count int `json:"count"`
}

type ReadAll struct {
	Count int `json:"count"`
}
