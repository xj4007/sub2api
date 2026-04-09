package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionRepositoryListByUserIDUsesReaderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	writerClient := newSettingRepoSQLiteClient(t, "file:user_sub_repo_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:user_sub_repo_reader?mode=memory&cache=shared")

	writerUser, err := writerClient.User.Create().
		SetEmail("sub-writer@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	readerUser, err := readerClient.User.Create().
		SetEmail("sub-reader@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	writerGroup, err := writerClient.Group.Create().SetName("writer-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	readerGroup, err := readerClient.Group.Create().SetName("reader-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = writerClient.UserSubscription.Create().
		SetUserID(writerUser.ID).
		SetGroupID(writerGroup.ID).
		SetStartsAt(now.Add(-1 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(ctx)
	require.NoError(t, err)

	_, err = readerClient.UserSubscription.Create().
		SetUserID(readerUser.ID).
		SetGroupID(readerGroup.ID).
		SetStartsAt(now.Add(-1 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(ctx)
	require.NoError(t, err)

	repo := NewUserSubscriptionRepository(writerClient, &ReaderEntClient{Client: readerClient}).(*userSubscriptionRepository)

	got, err := repo.ListByUserID(ctx, readerUser.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, readerGroup.ID, got[0].GroupID)
}

func TestUserSubscriptionRepositoryListActiveByUserIDUsesReaderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	writerClient := newSettingRepoSQLiteClient(t, "file:user_sub_active_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:user_sub_active_reader?mode=memory&cache=shared")

	writerUser, err := writerClient.User.Create().
		SetEmail("sub-active-writer@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	readerUser, err := readerClient.User.Create().
		SetEmail("sub-active-reader@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	writerGroup, err := writerClient.Group.Create().SetName("writer-active-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	readerGroup, err := readerClient.Group.Create().SetName("reader-active-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = writerClient.UserSubscription.Create().
		SetUserID(writerUser.ID).
		SetGroupID(writerGroup.ID).
		SetStartsAt(now.Add(-2 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(ctx)
	require.NoError(t, err)

	_, err = readerClient.UserSubscription.Create().
		SetUserID(readerUser.ID).
		SetGroupID(readerGroup.ID).
		SetStartsAt(now.Add(-2 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(ctx)
	require.NoError(t, err)

	repo := NewUserSubscriptionRepository(writerClient, &ReaderEntClient{Client: readerClient}).(*userSubscriptionRepository)

	got, err := repo.ListActiveByUserID(ctx, readerUser.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, readerGroup.ID, got[0].GroupID)
}

func TestUserSubscriptionRepositoryGetByIDUsesReaderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	writerClient := newSettingRepoSQLiteClient(t, "file:user_sub_get_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:user_sub_get_reader?mode=memory&cache=shared")

	writerUser, err := writerClient.User.Create().
		SetEmail("sub-get-writer@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	readerUser, err := readerClient.User.Create().
		SetEmail("sub-get-reader@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	writerGroup, err := writerClient.Group.Create().SetName("writer-get-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	readerGroup, err := readerClient.Group.Create().SetName("reader-get-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = writerClient.UserSubscription.Create().
		SetUserID(writerUser.ID).
		SetGroupID(writerGroup.ID).
		SetStartsAt(now.Add(-1 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(ctx)
	require.NoError(t, err)

	readerSub, err := readerClient.UserSubscription.Create().
		SetUserID(readerUser.ID).
		SetGroupID(readerGroup.ID).
		SetStartsAt(now.Add(-1 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(ctx)
	require.NoError(t, err)

	repo := NewUserSubscriptionRepository(writerClient, &ReaderEntClient{Client: readerClient}).(*userSubscriptionRepository)

	got, err := repo.GetByID(ctx, readerSub.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, readerGroup.ID, got.GroupID)
	require.NotNil(t, got.Group)
	require.Equal(t, "reader-get-group", got.Group.Name)
}
