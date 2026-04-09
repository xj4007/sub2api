//go:build unit

package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
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
