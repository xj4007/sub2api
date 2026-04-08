package repository

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newSettingRepoSQLiteClient(t *testing.T, dsn string) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func TestSettingRepositoryGetMultipleUsesReaderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	writerClient := newSettingRepoSQLiteClient(t, "file:setting_repo_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:setting_repo_reader?mode=memory&cache=shared")

	require.NoError(t, writerClient.Setting.Create().SetKey("site_name").SetValue("writer-site").Exec(ctx))
	require.NoError(t, readerClient.Setting.Create().SetKey("site_name").SetValue("reader-site").Exec(ctx))

	repo := NewSettingRepository(writerClient, &ReaderEntClient{Client: readerClient}).(*settingRepository)

	settings, err := repo.GetMultiple(ctx, []string{"site_name"})
	require.NoError(t, err)
	require.Equal(t, "reader-site", settings["site_name"])
}
