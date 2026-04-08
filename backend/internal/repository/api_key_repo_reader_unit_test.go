package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryListByUserIDUsesReaderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	writerClient := newSettingRepoSQLiteClient(t, "file:api_key_repo_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:api_key_repo_reader?mode=memory&cache=shared")

	writerUser, err := writerClient.User.Create().
		SetEmail("apikey-writer@test.com").
		SetPasswordHash("pw").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	readerUser, err := readerClient.User.Create().
		SetEmail("apikey-reader@test.com").
		SetPasswordHash("pw").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	_, err = writerClient.APIKey.Create().
		SetUserID(writerUser.ID).
		SetKey("sk-writer-only").
		SetName("writer-key").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	_, err = readerClient.APIKey.Create().
		SetUserID(readerUser.ID).
		SetKey("sk-reader-only").
		SetName("reader-key").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	repo := NewAPIKeyRepository(writerClient, nil, &ReaderEntClient{Client: readerClient}).(*apiKeyRepository)

	keys, page, err := repo.ListByUserID(ctx, readerUser.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, "sk-reader-only", keys[0].Key)
}
