package integrationtest

import (
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/email"
)

func (s *notificationSuite) TestSignInEmail_IsRecordedWithoutItsCodeAndSentOnce() {
	ayse := s.signIn("Ayse", "tr")
	message := &rabbitmq.Message{
		RoutingKey: constants.EmailVerificationCode, MessageID: uuid.NewString(),
		Payload: []byte(s.json(map[string]any{"payload": map[string]any{
			"user_id": ayse.ID, "email": ayse.Email, "full_name": ayse.FullName, "locale": "tr", "code": "482913",
		}})),
	}

	s.Require().NoError(s.deliver(message))
	s.Require().NoError(s.deliver(message), "the shovel redelivers the same message")

	sent := s.emailsTo(ayse)
	s.Require().Len(sent, 1)
	s.Equal(email.KindEmailVerificationCode, sent[0].Kind)
	s.Equal("482913", sent[0].Params["Code"])
	s.Equal(1, s.recorded("email_verification_code"), "every email is a Notification")
	s.Equal(0, s.recordedContaining("482913"), "the code is never recorded")
	s.Empty(s.inbox(ayse), "sign-in mail stays out of the Inbox")
}

func (s *notificationSuite) TestInvitation_ToAnAccountReachesItsInboxAndItsEmail() {
	ayse, can := s.signIn("Ayse", "tr"), s.signIn("Can", "en")
	teamID := s.team("Acme", ayse)
	invitationID := uuid.New()

	s.Require().NoError(s.invite(teamID, invitationID, can, can.ID, time.Now()))

	items := s.inbox(can)
	s.Require().Len(items, 1)
	s.Equal("team_invitation", items[0]["type"])
	s.Nil(items[0]["team_id"], "not a member yet, so the Inbox shows it whatever their Teams")
	params := items[0]["params"].(map[string]any)
	s.Equal(invitationID.String(), params["invitation_id"])
	s.Equal("Acme", params["team_name"])
	s.Equal("developer", params["role"])
	s.Equal("Ayse", params["inviter_name"])
	s.NotContains(params, "accept_url")
	sent := s.emailsTo(can)
	s.Require().Len(sent, 1)
	s.Contains(sent[0].Params["AcceptURL"], "secret-accept-token")
	s.Equal(0, s.recordedContaining("secret-accept-token"))
}

func (s *notificationSuite) TestInvitation_ToAnAddressWithoutAnAccountGoesByEmailAndAResendGoesAgain() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	stranger := user{Email: "newcomer@example.com", Locale: "en"}
	invitationID := uuid.New()

	s.Require().NoError(s.invite(teamID, invitationID, stranger, uuid.Nil, time.Now()))
	s.Require().NoError(s.invite(teamID, invitationID, stranger, uuid.Nil, time.Now().Add(time.Hour)))

	s.Len(s.emailsTo(stranger), 2, "the owner resent it")
}

func (s *notificationSuite) TestRoleChange_IsToldWhenSomeoneElseMadeIt() {
	ayse, mehmet := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")

	s.changeRole(teamID, mehmet, "finance", ayse)
	s.changeRole(teamID, ayse, "owner", ayse)

	items := s.inbox(mehmet)
	s.Require().Len(items, 1)
	s.Equal("team_role_changed", items[0]["type"])
	s.Equal(teamID.String(), items[0]["team_id"])
	s.Equal(map[string]any{"team_id": teamID.String(), "team_name": "Acme", "role": "finance"}, items[0]["params"])
	s.Empty(s.inbox(ayse), "a change one made oneself is no news")
}

func (s *notificationSuite) TestRemoval_IsToldAndStaysVisibleAfterTheTeamIsGone() {
	ayse, mehmet, zeynep := s.signIn("Ayse", "tr"), s.signIn("Mehmet", "tr"), s.signIn("Zeynep", "en")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, mehmet, "developer")
	s.addMember(teamID, zeynep, "finance")

	s.removeMemberBy(teamID, mehmet, ayse)
	s.removeMemberBy(teamID, zeynep, zeynep)

	items := s.inbox(mehmet)
	s.Require().Len(items, 1, "removed, yet the Notification of it stays in their Inbox")
	s.Equal("team_member_removed", items[0]["type"])
	s.Nil(items[0]["team_id"])
	s.Equal("Acme", items[0]["params"].(map[string]any)["team_name"])
	s.Empty(s.inbox(zeynep), "leaving is no news to the one who left")
}

func (s *notificationSuite) TestProfileUpdate_NextEmailsUseTheNewNameAndLanguage() {
	ayse, zeynep := s.signIn("Ayse", "tr"), s.signIn("Zeynep", "en")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, zeynep, "finance")

	s.updateProfile(zeynep, "Zeynep Kaya", "tr", time.Now())
	s.updateProfile(zeynep, "Old Name", "en", time.Now().Add(-time.Hour))
	s.Require().NoError(s.failed(newDroplet(teamID, "web-1"), "rejected"))

	sent := s.emailsTo(zeynep)
	s.Require().Len(sent, 1)
	s.Equal("tr", sent[0].Locale)
	s.Equal("Zeynep Kaya", sent[0].Params["FullName"], "an older profile change arriving later changes nothing")
}

func (s *notificationSuite) invite(teamID, invitationID uuid.UUID, to user, toUserID uuid.UUID, sentAt time.Time) error {
	return s.consumeErr(constants.TeamInvitation, s.json(map[string]any{"payload": map[string]any{
		"invitation_id": invitationID, "team_id": teamID, "sent_at": sentAt.UTC(),
		"recipient": map[string]any{"user_id": toUserID, "email": to.Email, "full_name": to.FullName, "locale": to.Locale},
		"team_name": "Acme", "role": "developer", "inviter_name": "Ayse",
		"accept_url": "http://localhost:3002/invitations/accept?token=secret-accept-token",
		"expires_at": sentAt.Add(7 * 24 * time.Hour).UTC(),
	}}))
}

func (s *notificationSuite) changeRole(teamID uuid.UUID, member user, role string, by user) {
	s.consume(constants.TeamMemberRoleChanged, s.json(map[string]any{"payload": map[string]any{
		"team_id": teamID, "user_id": member.ID, "role": role, "changed_at": time.Now().UTC(), "changed_by": by.ID,
	}}))
}

func (s *notificationSuite) removeMemberBy(teamID uuid.UUID, member, by user) {
	s.consume(constants.TeamMemberRemoved, s.json(map[string]any{"payload": map[string]any{
		"team_id": teamID, "user_id": member.ID, "removed_at": time.Now().UTC(), "removed_by": by.ID,
	}}))
}

func (s *notificationSuite) updateProfile(u user, fullName, locale string, at time.Time) {
	s.consume(constants.UserProfileUpdated, s.json(map[string]any{"payload": map[string]any{
		"user_id": u.ID, "full_name": fullName, "locale": locale, "updated_at": at.UTC(),
	}}))
}

func (s *notificationSuite) recorded(notificationType string) int {
	var count int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM notifications WHERE type = $1`, notificationType).Scan(&count))

	return count
}
