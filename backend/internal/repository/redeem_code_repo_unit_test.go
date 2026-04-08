package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRedeemCodeRepositoryListByUserUsesReaderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	writerClient := newSettingRepoSQLiteClient(t, "file:redeem_repo_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:redeem_repo_reader?mode=memory&cache=shared")

	writerUser, err := writerClient.User.Create().
		SetEmail("redeem-writer@test.com").
		SetPasswordHash("pw").
		Save(ctx)
	require.NoError(t, err)

	readerUser, err := readerClient.User.Create().
		SetEmail("redeem-reader@test.com").
		SetPasswordHash("pw").
		Save(ctx)
	require.NoError(t, err)

	usedAt := time.Now().UTC().Truncate(time.Second)

	_, err = writerClient.RedeemCode.Create().
		SetCode("WRITER-HISTORY").
		SetType(service.RedeemTypeBalance).
		SetStatus(service.StatusUsed).
		SetValue(10).
		SetNotes("").
		SetValidityDays(30).
		SetUsedBy(writerUser.ID).
		SetUsedAt(usedAt).
		Save(ctx)
	require.NoError(t, err)

	_, err = readerClient.RedeemCode.Create().
		SetCode("READER-HISTORY").
		SetType(service.RedeemTypeBalance).
		SetStatus(service.StatusUsed).
		SetValue(20).
		SetNotes("").
		SetValidityDays(30).
		SetUsedBy(readerUser.ID).
		SetUsedAt(usedAt).
		Save(ctx)
	require.NoError(t, err)

	repo := NewRedeemCodeRepository(writerClient, &ReaderEntClient{Client: readerClient}).(*redeemCodeRepository)

	results, err := repo.ListByUser(ctx, readerUser.ID, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "READER-HISTORY", results[0].Code)
}
