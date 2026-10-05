package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/application/handlers"
	mockservices "github.com/piggystats/nodera/contexts/customer/mock/services"
	"github.com/piggystats/nodera/logger"
	mockpersistence "github.com/piggystats/nodera/mock/persistence"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/constants"
)

type apiKeyFixture struct {
	teams        *mockservices.MockITeamService
	projects     *mockservices.MockIProjectService
	keys         *mockservices.MockIAPIKeyService
	transactions *mockpersistence.MockITransactionManager
	entries      *mockservices.MockIAPIKeyEntries
	handler      *handlers.APIKey
	ctx          context.Context
	teamID       uuid.UUID
	projectID    uuid.UUID
	userID       uuid.UUID
}

func newAPIKeyFixture(t *testing.T) *apiKeyFixture {
	ctrl := gomock.NewController(t)
	f := &apiKeyFixture{
		teams:        mockservices.NewMockITeamService(ctrl),
		projects:     mockservices.NewMockIProjectService(ctrl),
		keys:         mockservices.NewMockIAPIKeyService(ctrl),
		transactions: mockpersistence.NewMockITransactionManager(ctrl),
		entries:      mockservices.NewMockIAPIKeyEntries(ctrl),
		teamID:       uuid.New(),
		projectID:    uuid.New(),
		userID:       uuid.New(),
	}
	f.ctx = context.WithValue(context.Background(), constants.CurrentUserID, f.userID.String())
	f.handler = handlers.NewAPIKey(logger.Noop(), f.teams, f.projects, f.keys, f.transactions, f.entries)

	return f
}

func (f *apiKeyFixture) member(role team.Role) {
	f.teams.EXPECT().FindMembership(gomock.Any(), f.teamID, f.userID).
		Return(&team.Membership{Team: team.Team{ID: f.teamID}, Role: role}, nil)
}

// lockedWith opens the creating transaction with the Project locked and
// already holding active keys.
func (f *apiKeyFixture) lockedWith(active int) {
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.projects.EXPECT().Lock(gomock.Any(), f.teamID, f.projectID).Return(nil)
	f.keys.EXPECT().CountActive(gomock.Any(), f.projectID).Return(active, nil)
}

func (f *apiKeyFixture) projectInTeam() {
	f.projects.EXPECT().FindInTeam(gomock.Any(), f.teamID, f.projectID).
		Return(&project.Project{ID: f.projectID, TeamID: f.teamID}, nil)
}

func (f *apiKeyFixture) createRequest(permissions ...string) *apikey.CreateRequest {
	return &apikey.CreateRequest{TeamID: f.teamID.String(), ProjectID: f.projectID.String(), Name: "production", Permissions: permissions}
}

func (f *apiKeyFixture) revokeRequest(keyID string) *apikey.RevokeRequest {
	return &apikey.RevokeRequest{TeamID: f.teamID.String(), ProjectID: f.projectID.String(), KeyID: keyID}
}

func httpStatusOf(t *testing.T, err error) int {
	t.Helper()
	var rerr rerror.Error
	require.True(t, errors.As(err, &rerr), "not an rerror: %v", err)

	return rerr.HttpStatus
}

func TestCreate_KeepsOnlyTheHashAndWritesTheEntryAfterTheCommit(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleDeveloper)
	f.lockedWith(apikey.MaxActivePerProject - 1)
	var stored *apikey.APIKey
	gomock.InOrder(
		f.keys.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key *apikey.APIKey) error {
			stored = key
			key.CreatedAt = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

			return nil
		}),
		f.transactions.EXPECT().Commit(gomock.Any()).Return(nil),
		f.entries.EXPECT().Write(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key *apikey.APIKey) error {
			assert.Same(t, stored, key, "the entry describes the stored row")

			return nil
		}),
	)

	created, err := f.handler.Create(f.ctx, f.createRequest("mail.send"))

	require.NoError(t, err)
	id, secret, found := strings.Cut(strings.TrimPrefix(created.Key, apikey.Marker), ".")
	require.True(t, found, created.Key)
	assert.Equal(t, stored.ID, id)
	assert.Equal(t, id, created.ID)
	assert.Equal(t, "nd_"+id, created.Prefix)
	assert.Equal(t, apikey.HashSecret(secret), stored.SecretHash)
	assert.Equal(t, []authz.Permission{authz.MailSend}, stored.Permissions)
	assert.Equal(t, []string{"mail.send"}, created.Permissions)
	assert.Equal(t, f.teamID, stored.TeamID)
	assert.Equal(t, f.projectID, stored.ProjectID)
	assert.Equal(t, f.userID, stored.CreatedBy)
	assert.Equal(t, f.userID, created.CreatedBy)
	assert.Nil(t, created.LastUsedAt)
}

func TestCreate_APermissionNoKeyMayCarryIsABadRequest(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleOwner)

	_, err := f.handler.Create(f.ctx, f.createRequest("mail.send", "mail.write"))

	assert.ErrorIs(t, err, apikey.ErrorPermissionNotAllowed)
	assert.Equal(t, http.StatusBadRequest, httpStatusOf(t, err))
}

func TestCreate_FinanceIsForbidden(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleFinance)

	_, err := f.handler.Create(f.ctx, f.createRequest("mail.send"))

	assert.ErrorIs(t, err, team.ErrorForbidden)
}

func TestCreate_AProjectOutsideTheTeamIsNotFound(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleOwner)
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.projects.EXPECT().Lock(gomock.Any(), f.teamID, f.projectID).Return(project.ErrorNotFound)
	f.transactions.EXPECT().Rollback(gomock.Any()).Return(nil)

	_, err := f.handler.Create(f.ctx, f.createRequest("mail.send"))

	assert.ErrorIs(t, err, project.ErrorNotFound)
	assert.Equal(t, http.StatusNotFound, httpStatusOf(t, err))
}

func TestCreate_TheTwentyFirstActiveKeyIsAConflict(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleDeveloper)
	f.lockedWith(apikey.MaxActivePerProject)
	f.transactions.EXPECT().Rollback(gomock.Any()).Return(nil)

	_, err := f.handler.Create(f.ctx, f.createRequest("mail.send"))

	assert.ErrorIs(t, err, apikey.ErrorLimitReached)
	assert.Equal(t, http.StatusConflict, httpStatusOf(t, err))
}

func TestCreate_AnEntryThatCannotBeWrittenWithdrawsTheKeyAndShowsNoSecret(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleDeveloper)
	f.lockedWith(0)
	var stored *apikey.APIKey
	f.keys.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key *apikey.APIKey) error {
		stored = key

		return nil
	})
	f.transactions.EXPECT().Commit(gomock.Any()).Return(nil)
	f.entries.EXPECT().Write(gomock.Any(), gomock.Any()).Return(errors.New("redis is down"))
	f.keys.EXPECT().Revoke(gomock.Any(), f.teamID, f.projectID, gomock.Any()).DoAndReturn(func(_ context.Context, _, _ uuid.UUID, id string) error {
		assert.Equal(t, stored.ID, id)

		return nil
	})

	created, err := f.handler.Create(f.ctx, f.createRequest("mail.send"))

	assert.Nil(t, created)
	assert.ErrorIs(t, err, apikey.ErrorCannotCreate)
	assert.Equal(t, http.StatusInternalServerError, httpStatusOf(t, err))
}

func TestRevoke_DeletesTheEntryBeforeTheRevocationCommits(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleDeveloper)
	f.projectInTeam()
	gomock.InOrder(
		f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil }),
		f.keys.EXPECT().Revoke(gomock.Any(), f.teamID, f.projectID, "AbCdEf012345").Return(nil),
		f.entries.EXPECT().Delete(gomock.Any(), "AbCdEf012345").Return(nil),
		f.transactions.EXPECT().Commit(gomock.Any()).Return(nil),
	)

	require.NoError(t, f.handler.Revoke(f.ctx, f.revokeRequest("AbCdEf012345")))
}

func TestRevoke_AnUnknownOrRevokedKeyIsNotFoundAndRedisIsLeftAlone(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleOwner)
	f.projectInTeam()
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.keys.EXPECT().Revoke(gomock.Any(), f.teamID, f.projectID, "AbCdEf012345").Return(apikey.ErrorNotFound)
	f.transactions.EXPECT().Rollback(gomock.Any()).Return(nil)

	err := f.handler.Revoke(f.ctx, f.revokeRequest("AbCdEf012345"))

	assert.ErrorIs(t, err, apikey.ErrorNotFound)
}

func TestRevoke_AnEntryThatCannotBeDeletedLeavesTheKeyActive(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleOwner)
	f.projectInTeam()
	f.transactions.EXPECT().Begin(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, error) { return ctx, nil })
	f.keys.EXPECT().Revoke(gomock.Any(), f.teamID, f.projectID, "AbCdEf012345").Return(nil)
	f.entries.EXPECT().Delete(gomock.Any(), "AbCdEf012345").Return(errors.New("redis is down"))
	f.transactions.EXPECT().Rollback(gomock.Any()).Return(nil)

	err := f.handler.Revoke(f.ctx, f.revokeRequest("AbCdEf012345"))

	assert.ErrorIs(t, err, apikey.ErrorCannotRevoke)
	assert.Equal(t, http.StatusInternalServerError, httpStatusOf(t, err))
}

func TestList_ShowsWhenEachKeyLastAuthenticated(t *testing.T) {
	f := newAPIKeyFixture(t)
	f.member(team.RoleDeveloper)
	f.projectInTeam()
	used := &apikey.APIKey{ID: "Used01234567", Permissions: []authz.Permission{authz.MailSend}}
	unused := &apikey.APIKey{ID: "Unused012345", Permissions: []authz.Permission{authz.MailSend}}
	lastUse := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	f.keys.EXPECT().FindActiveByProjectID(gomock.Any(), f.projectID).Return(apikey.APIKeys{used, unused}, nil)
	f.entries.EXPECT().LastUsed(gomock.Any(), []string{"Used01234567", "Unused012345"}).
		Return(map[string]time.Time{"Used01234567": lastUse}, nil)

	keys, err := f.handler.List(f.ctx, &apikey.ListRequest{TeamID: f.teamID.String(), ProjectID: f.projectID.String()})

	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.NotNil(t, keys[0].LastUsedAt)
	assert.Equal(t, lastUse, *keys[0].LastUsedAt)
	assert.Nil(t, keys[1].LastUsedAt)
}

func TestAPIKeySync_ReadsTheRevokedKeysOnlyAfterWritingTheActiveOnes(t *testing.T) {
	ctrl := gomock.NewController(t)
	keys := mockservices.NewMockIAPIKeyService(ctrl)
	entries := mockservices.NewMockIAPIKeyEntries(ctrl)
	first := &apikey.APIKey{ID: "Active000001"}
	second := &apikey.APIKey{ID: "Active000002"}
	gomock.InOrder(
		keys.EXPECT().FindAllActive(gomock.Any()).Return(apikey.APIKeys{first, second}, nil),
		entries.EXPECT().Write(gomock.Any(), first).Return(nil),
		entries.EXPECT().Write(gomock.Any(), second).Return(nil),
		keys.EXPECT().FindAllRevoked(gomock.Any()).Return(apikey.APIKeys{{ID: "Revoked00001"}}, nil),
		entries.EXPECT().Delete(gomock.Any(), "Revoked00001").Return(nil),
	)

	summary, err := handlers.NewAPIKeySync(keys, entries).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, &handlers.APIKeySyncSummary{Written: 2, Deleted: 1}, summary)
}
