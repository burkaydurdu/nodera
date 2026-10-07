package integrationtest

import (
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/email"
)

// teamStorage is a Storage of a Team's Project as the storage context names it.
type teamStorage struct {
	ID          uuid.UUID
	TeamID      uuid.UUID
	ProjectID   uuid.UUID
	ProjectName string
	BucketName  string
}

func newStorage(teamID uuid.UUID) teamStorage {
	return teamStorage{ID: uuid.New(), TeamID: teamID, ProjectID: uuid.New(), ProjectName: "Shop", BucketName: "shop-assets"}
}

// storageCase is one of storage's events and the Notification it makes, its
// params as the Inbox shows them.
type storageCase struct {
	routingKey string
	kind       email.Kind
	fields     map[string]any
	params     map[string]any
}

func storageCases(st teamStorage) []storageCase {
	at := time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC)
	storage := func(more map[string]any) map[string]any {
		out := map[string]any{
			"project_id": st.ProjectID.String(), "project_name": st.ProjectName, "storage_id": st.ID.String(), "bucket_name": st.BucketName,
		}
		maps.Copy(out, more)

		return out
	}

	return []storageCase{
		{
			constants.StorageNearlyFull, email.KindStorageNearlyFull,
			map[string]any{"package_code": "10gb", "size_bytes": 10_000_000_000, "used_bytes": 8_100_000_000, "at": at},
			storage(map[string]any{"package_code": "10gb", "size_bytes": float64(10_000_000_000), "used_bytes": float64(8_100_000_000)}),
		},
		{
			constants.StorageFull, email.KindStorageFull,
			map[string]any{"package_code": "10gb", "size_bytes": 10_000_000_000, "used_bytes": 10_000_000_000, "at": at.Add(time.Hour)},
			storage(map[string]any{"package_code": "10gb", "size_bytes": float64(10_000_000_000), "used_bytes": float64(10_000_000_000)}),
		},
		{
			constants.StoragePaused, email.KindStoragePaused,
			map[string]any{"reason": "abuse_complaint", "paused_at": at},
			storage(map[string]any{"reason": "abuse_complaint"}),
		},
		{
			constants.StorageDowngradeSkipped, email.KindStorageDowngradeSkipped,
			map[string]any{
				"package_code": "50gb", "skipped_package_code": "10gb", "skipped_size_bytes": 10_000_000_000, "used_bytes": 12_300_000_000, "at": at,
			},
			storage(map[string]any{
				"package_code": "50gb", "skipped_package_code": "10gb", "skipped_size_bytes": float64(10_000_000_000), "used_bytes": float64(12_300_000_000),
			}),
		},
	}
}

// storageMessage is the storage context publishing one of its events about the
// Storage, through its outbox.
func (s *notificationSuite) storageMessage(routingKey string, st teamStorage, fields map[string]any) *rabbitmq.Message {
	payload := map[string]any{
		"team_id": st.TeamID, "project_id": st.ProjectID, "project_name": st.ProjectName, "storage_id": st.ID, "bucket_name": st.BucketName,
	}
	maps.Copy(payload, fields)

	return &rabbitmq.Message{
		RoutingKey: routingKey, Payload: []byte(s.json(map[string]any{"payload": payload})),
		MessageID: uuid.NewString(), CorrelationID: uuid.NewString(),
	}
}

func (s *notificationSuite) TestStorageEvents_ReachEveryStorageReaderOfTheTeamAndNobodyElse() {
	ayse, mehmet, zeynep := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr"), s.signIn("Zeynep", "en")
	former, stranger := s.signIn("Deniz", "tr"), s.signIn("Can", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")
	s.addMember(teamID, zeynep, "finance")
	s.addMember(teamID, former, "developer")
	s.removeMember(teamID, former)
	s.team("Other", stranger)
	assets := newStorage(teamID)
	cases := storageCases(assets)

	for _, c := range cases {
		s.Require().NoError(s.deliver(s.storageMessage(c.routingKey, assets, c.fields)), c.routingKey)
	}

	for _, member := range []user{ayse, mehmet, zeynep} {
		items := s.inbox(member)
		s.Require().Len(items, len(cases), member.FullName)
		params := map[string]any{}
		for _, item := range items {
			s.Equal(teamID.String(), item["team_id"])
			params[item["type"].(string)] = item["params"]
		}
		for _, c := range cases {
			s.Equal(c.params, params[string(c.kind)], "%s for %s: ids, names, numbers and codes, never a sentence", c.kind, member.FullName)
		}
	}
	for _, c := range cases {
		s.Equal(3, s.recorded(string(c.kind)), "%s: the owner, the developer and finance, each holding storage.read", c.kind)
	}
	for _, outsider := range []user{former, stranger} {
		s.Empty(s.emailsTo(outsider), outsider.FullName)
		s.Empty(s.pushedTo(outsider, "notification.created"), outsider.FullName)
		s.Zero(s.recordedFor(outsider), "%s is outside the Team", outsider.FullName)
	}
}

func (s *notificationSuite) TestStorageEvents_GoOutInTheAppAndByEmailOnceEachInTheReadersLanguage() {
	ayse, zeynep := s.signIn("Ayse", "tr"), s.signIn("Zeynep", "en")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, zeynep, "finance")
	assets := newStorage(teamID)
	cases := storageCases(assets)
	kinds := make([]email.Kind, 0, len(cases))

	for _, c := range cases {
		s.Require().NoError(s.deliver(s.storageMessage(c.routingKey, assets, c.fields)), c.routingKey)
		kinds = append(kinds, c.kind)
	}

	link := "http://localhost:3002/dashboard/storage?team=" + teamID.String() + "&project=" + assets.ProjectID.String() + "&storage=" + assets.ID.String()
	for _, member := range []user{ayse, zeynep} {
		s.Len(s.pushedTo(member, "notification.created"), len(cases), member.FullName)
		sent := s.emailsTo(member)
		s.Require().Len(sent, len(cases), member.FullName)
		sentKinds := make([]email.Kind, 0, len(sent))
		for _, e := range sent {
			sentKinds = append(sentKinds, e.Kind)
			s.Equal(member.Locale, e.Locale, "each member in their own language")
			s.Equal(member.FullName, e.Params["FullName"])
			s.Equal("Acme", e.Params["TeamName"])
			s.Equal("Shop", e.Params["ProjectName"])
			s.Equal("shop-assets", e.Params["BucketName"])
			s.Equal(link, e.Params["StorageURL"])
		}
		s.ElementsMatch(kinds, sentKinds)
	}
	s.Equal(2*len(cases)*2, s.deliveries(), "an in-app and an email delivery per member and event")
	s.Zero(s.undelivered())
	s.Zero(s.recordedContaining("/dashboard/storage"), "the link rides in the email only")
}

func (s *notificationSuite) TestStorageEvents_ARedeliveredEventIsToldOnce() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	assets := newStorage(teamID)
	cases := storageCases(assets)

	for _, c := range cases {
		message := s.storageMessage(c.routingKey, assets, c.fields)
		s.Require().NoError(s.deliver(message), c.routingKey)
		s.Require().NoError(s.deliver(message), "the shovel redelivers %s", c.routingKey)
	}

	s.Len(s.inbox(ayse), len(cases))
	s.Len(s.pushedTo(ayse, "notification.created"), len(cases))
	s.Len(s.emailsTo(ayse), len(cases))

	filledAgain := maps.Clone(cases[0].fields)
	filledAgain["at"] = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	s.Require().NoError(s.deliver(s.storageMessage(constants.StorageNearlyFull, assets, filledAgain)))
	s.Len(s.inbox(ayse), len(cases)+1, "a Storage that emptied and fills up a second time is news")
}

func (s *notificationSuite) TestStorageEvents_AMalformedPayloadIsRefused() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	assets := newStorage(teamID)
	at := time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC)
	fill := map[string]any{"package_code": "10gb", "size_bytes": 10_000_000_000, "used_bytes": 8_100_000_000, "at": at}
	teamless, nameless := assets, assets
	teamless.TeamID, nameless.ID = uuid.Nil, uuid.Nil

	for _, message := range []*rabbitmq.Message{
		{RoutingKey: constants.StorageNearlyFull, Payload: []byte("not json"), MessageID: uuid.NewString()},
		s.storageMessage(constants.StorageNearlyFull, teamless, fill),
		s.storageMessage(constants.StorageFull, nameless, fill),
		s.storageMessage(constants.StorageFull, assets, map[string]any{"package_code": "10gb", "size_bytes": 10_000_000_000, "used_bytes": 10_000_000_000}),
		s.storageMessage(constants.StoragePaused, assets, map[string]any{"reason": "bad_taste", "paused_at": at}),
		s.storageMessage(constants.StorageDowngradeSkipped, assets,
			map[string]any{"package_code": "50gb", "skipped_size_bytes": 10_000_000_000, "used_bytes": 12_300_000_000, "at": at}),
	} {
		err := s.deliver(message)
		s.Require().ErrorIs(err, notification.ErrorMalformedEvent, string(message.Payload))
		var refused rerror.Error
		s.Require().ErrorAs(err, &refused)
		s.Equal(notification.ErrorMalformedEvent.HttpStatus, refused.HttpStatus, "terminal: the same bytes would fail again")
	}

	s.Zero(s.deliveries(), "nothing is recorded")
	s.Empty(s.email.sent)
	s.Empty(s.realtime.messages)
}
