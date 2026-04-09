//go:build unit

package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSafeDateFormat(t *testing.T) {
	tests := []struct {
		name        string
		granularity string
		expected    string
	}{
		// 合法值
		{"hour", "hour", "YYYY-MM-DD HH24:00"},
		{"day", "day", "YYYY-MM-DD"},
		{"week", "week", "IYYY-IW"},
		{"month", "month", "YYYY-MM"},

		// 非法值回退到默认
		{"空字符串", "", "YYYY-MM-DD"},
		{"未知粒度 year", "year", "YYYY-MM-DD"},
		{"未知粒度 minute", "minute", "YYYY-MM-DD"},

		// 恶意字符串
		{"SQL 注入尝试", "'; DROP TABLE users; --", "YYYY-MM-DD"},
		{"带引号", "day'", "YYYY-MM-DD"},
		{"带括号", "day)", "YYYY-MM-DD"},
		{"Unicode", "日", "YYYY-MM-DD"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeDateFormat(tc.granularity)
			require.Equal(t, tc.expected, got, "safeDateFormat(%q)", tc.granularity)
		})
	}
}

func TestBuildUsageLogBatchInsertQuery_UsesConflictDoNothing(t *testing.T) {
	log := &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    "req-batch-no-update",
		Model:        "gpt-5",
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.2,
		ActualCost:   1.2,
		CreatedAt:    time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(log)

	query, _ := buildUsageLogBatchInsertQuery([]string{usageLogBatchKey(log.RequestID, log.APIKeyID)}, map[string]usageLogInsertPrepared{
		usageLogBatchKey(log.RequestID, log.APIKeyID): prepared,
	})

	require.Contains(t, query, "ON CONFLICT (request_id, api_key_id) DO NOTHING")
	require.NotContains(t, strings.ToUpper(query), "DO UPDATE")
}

func TestUsageLogRepositoryReadSQLUsesReaderWhenConfigured(t *testing.T) {
	writer := &sql.DB{}
	reader := &sql.DB{}
	repo := &usageLogRepository{
		sql:       writer,
		db:        writer,
		readerSQL: reader,
		readerDB:  reader,
	}

	require.Same(t, reader, repo.readSQL())
	require.Same(t, reader, repo.readDB())
}

func TestUsageLogRepositoryReadSQLFallsBackToWriter(t *testing.T) {
	writer := &sql.DB{}
	repo := &usageLogRepository{
		sql: writer,
		db:  writer,
	}

	require.Same(t, writer, repo.readSQL())
	require.Same(t, writer, repo.readDB())
}

func TestGetUserDashboardStatsUsesReaderForAllQueries(t *testing.T) {
	writerDB, writerMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer writerDB.Close()

	readerDB, readerMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer readerDB.Close()

	repo := &usageLogRepository{
		sql:       writerDB,
		db:        writerDB,
		readerSQL: readerDB,
		readerDB:  readerDB,
	}

	readerMock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_keys WHERE user_id = \$1 AND deleted_at IS NULL`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))

	readerMock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_keys WHERE user_id = \$1 AND status = \$2 AND deleted_at IS NULL`).
		WithArgs(int64(42), service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))

	readerMock.ExpectQuery(`FROM usage_logs\s+WHERE user_id = \$1`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_creation_tokens",
			"total_cache_read_tokens",
			"total_cost",
			"total_actual_cost",
			"avg_duration_ms",
		}).AddRow(int64(10), int64(100), int64(200), int64(10), int64(5), float64(12.5), float64(10.5), float64(123.4)))

	readerMock.ExpectQuery(`FROM usage_logs\s+WHERE user_id = \$1 AND created_at >= \$2`).
		WithArgs(int64(42), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"today_requests",
			"today_input_tokens",
			"today_output_tokens",
			"today_cache_creation_tokens",
			"today_cache_read_tokens",
			"today_cost",
			"today_actual_cost",
		}).AddRow(int64(4), int64(40), int64(80), int64(4), int64(2), float64(5.5), float64(4.5)))

	readerMock.ExpectQuery(`FROM usage_logs\s+WHERE created_at >= \$1 AND user_id = \$2`).
		WithArgs(sqlmock.AnyArg(), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count"}).AddRow(int64(15), int64(300)))

	stats, err := repo.GetUserDashboardStats(t.Context(), 42)
	require.NoError(t, err)
	require.NotNil(t, stats)
	require.Equal(t, int64(3), stats.TotalAPIKeys)
	require.Equal(t, int64(2), stats.ActiveAPIKeys)
	require.Equal(t, int64(10), stats.TotalRequests)
	require.Equal(t, int64(315), stats.TotalTokens)
	require.Equal(t, int64(4), stats.TodayRequests)
	require.Equal(t, int64(126), stats.TodayTokens)
	require.Equal(t, int64(3), stats.Rpm)
	require.Equal(t, int64(60), stats.Tpm)

	require.NoError(t, readerMock.ExpectationsWereMet())
	require.NoError(t, writerMock.ExpectationsWereMet())
}

func TestListWithFiltersUsesReaderWhenConfigured(t *testing.T) {
	writerDB, writerMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer writerDB.Close()

	readerDB, readerMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer readerDB.Close()

	repo := &usageLogRepository{
		sql:       writerDB,
		db:        writerDB,
		readerSQL: readerDB,
		readerDB:  readerDB,
	}

	readerMock.ExpectQuery(`SELECT COUNT\(\*\) FROM usage_logs WHERE user_id = \$1`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))

	readerMock.ExpectQuery(`SELECT .* FROM usage_logs WHERE user_id = \$1 ORDER BY id DESC LIMIT \$2 OFFSET \$3`).
		WithArgs(int64(42), 20, 0).
		WillReturnRows(sqlmock.NewRows(strings.Split(usageLogSelectColumns, ", ")))

	logs, page, err := repo.ListWithFilters(t.Context(), pagination.PaginationParams{Page: 1, PageSize: 20}, UsageLogFilters{UserID: 42})
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.Equal(t, int64(0), page.Total)

	require.NoError(t, readerMock.ExpectationsWereMet())
	require.NoError(t, writerMock.ExpectationsWereMet())
}

func TestHydrateUsageLogAssociationsUsesReaderWhenConfigured(t *testing.T) {
	ctx := t.Context()
	writerClient := newSettingRepoSQLiteClient(t, "file:usage_repo_writer?mode=memory&cache=shared")
	readerClient := newSettingRepoSQLiteClient(t, "file:usage_repo_reader?mode=memory&cache=shared")

	writerUser, err := writerClient.User.Create().
		SetEmail("usage-writer@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	readerUser, err := readerClient.User.Create().
		SetEmail("usage-reader@test.com").
		SetPasswordHash("pw").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	writerAccount, err := writerClient.Account.Create().
		SetName("writer-account").
		SetPlatform(service.PlatformAnthropic).
		SetType(service.AccountTypeAPIKey).
		SetCredentials(map[string]any{}).
		SetExtra(map[string]any{}).
		SetConcurrency(1).
		SetPriority(1).
		SetStatus(service.StatusActive).
		SetSchedulable(true).
		SetErrorMessage("").
		Save(ctx)
	require.NoError(t, err)

	readerAccount, err := readerClient.Account.Create().
		SetName("reader-account").
		SetPlatform(service.PlatformAnthropic).
		SetType(service.AccountTypeAPIKey).
		SetCredentials(map[string]any{}).
		SetExtra(map[string]any{}).
		SetConcurrency(1).
		SetPriority(1).
		SetStatus(service.StatusActive).
		SetSchedulable(true).
		SetErrorMessage("").
		Save(ctx)
	require.NoError(t, err)

	_, err = writerClient.APIKey.Create().
		SetUserID(writerUser.ID).
		SetKey("sk-usage-writer").
		SetName("writer-key").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	readerKey, err := readerClient.APIKey.Create().
		SetUserID(readerUser.ID).
		SetKey("sk-usage-reader").
		SetName("reader-key").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	logs := []service.UsageLog{{
		UserID:    readerUser.ID,
		APIKeyID:  readerKey.ID,
		AccountID: readerAccount.ID,
	}}

	repo := &usageLogRepository{client: writerClient, readerClient: readerClient}

	require.NoError(t, repo.hydrateUsageLogAssociations(ctx, logs))
	require.NotNil(t, logs[0].User)
	require.Equal(t, "usage-reader@test.com", logs[0].User.Email)
	require.NotNil(t, logs[0].APIKey)
	require.Equal(t, "reader-key", logs[0].APIKey.Name)
	require.NotNil(t, logs[0].Account)
	require.Equal(t, "reader-account", logs[0].Account.Name)

	_ = writerAccount
}
