package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryGetByIDUsesReaderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	writerClient := newSettingRepoSQLiteClient(t, "file:user_repo_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:user_repo_reader?mode=memory&cache=shared")

	writerUser, err := writerClient.User.Create().
		SetEmail("user-writer@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	readerUser, err := readerClient.User.Create().
		SetEmail("user-reader@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	writerGroup, err := writerClient.Group.Create().SetName("writer-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	readerGroup, err := readerClient.Group.Create().SetName("reader-group").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)

	err = writerClient.UserAllowedGroup.Create().SetUserID(writerUser.ID).SetGroupID(writerGroup.ID).Exec(ctx)
	require.NoError(t, err)
	err = readerClient.UserAllowedGroup.Create().SetUserID(readerUser.ID).SetGroupID(readerGroup.ID).Exec(ctx)
	require.NoError(t, err)

	repo := NewUserRepository(writerClient, nil, &ReaderEntClient{Client: readerClient}).(*userRepository)

	got, err := repo.GetByID(ctx, readerUser.ID)
	require.NoError(t, err)
	require.Equal(t, "user-reader@test.com", got.Email)
	require.Equal(t, []int64{readerGroup.ID}, got.AllowedGroups)
}
