package model

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestNormalizeUsageLog(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		row                  usageLogRow
		input, output        int64
		read, write          *int64
		ambiguous, estimated int64
	}{
		{name: "openai inclusive input", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":60}`}, input: 100, output: 20, read: common.GetPointer(int64(60))},
		{name: "recorded zero is distinct from missing", row: usageLogRow{PromptTokens: 100, Other: `{"cache_tokens":0}`}, input: 100, read: common.GetPointer(int64(0))},
		{name: "missing cache stays unknown", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20, Other: `{}`}, input: 100, output: 20},
		{name: "missing metadata stays unknown", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20}, input: 100, output: 20},
		{name: "gemini inclusive input", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20, Other: `{"usage_semantic":"gemini","cache_tokens":60}`}, input: 100, output: 20, read: common.GetPointer(int64(60))},
		{name: "explicit inclusive total wins", row: usageLogRow{PromptTokens: 10, CompletionTokens: 20, Other: `{"input_tokens_total":100,"usage_semantic":"anthropic","cache_tokens":60,"cache_creation_tokens":30}`}, input: 100, output: 20, read: common.GetPointer(int64(60)), write: common.GetPointer(int64(30))},
		{name: "explicit zero total is not absent", row: usageLogRow{PromptTokens: 100, Other: `{"input_tokens_total":0,"cache_tokens":0}`}, read: common.GetPointer(int64(0))},
		{name: "anthropic disjoint inputs", row: usageLogRow{PromptTokens: 10, CompletionTokens: 20, Other: `{"usage_semantic":"anthropic","cache_tokens":60,"cache_write_tokens":30,"cache_creation_tokens":30,"cache_creation_tokens_5m":20,"cache_creation_tokens_1h":10}`}, input: 100, output: 20, read: common.GetPointer(int64(60)), write: common.GetPointer(int64(30))},
		{name: "legacy claude marker", row: usageLogRow{PromptTokens: 10, Other: `{"claude":true,"cache_tokens":60,"cache_creation_tokens_5m":20,"cache_creation_tokens_1h":10}`}, input: 100, read: common.GetPointer(int64(60)), write: common.GetPointer(int64(30))},
		{name: "write aggregate can exceed split total", row: usageLogRow{PromptTokens: 10, Other: `{"claude":true,"cache_creation_tokens":50,"cache_creation_tokens_5m":20,"cache_creation_tokens_1h":10}`}, input: 60, write: common.GetPointer(int64(50))},
		{name: "openai read write overlap is not added to input", row: usageLogRow{PromptTokens: 100, Other: `{"cache_tokens":80,"cache_write_tokens":60}`}, input: 100, read: common.GetPointer(int64(80)), write: common.GetPointer(int64(60))},
		{name: "ambiguous old claude conversion", row: usageLogRow{PromptTokens: 10, CompletionTokens: 20, Other: `{"cache_tokens":60,"cache_creation_tokens_5m":30}`}, output: 20, ambiguous: 1},
		{name: "inconsistent cache exceeds inclusive input", row: usageLogRow{PromptTokens: 10, CompletionTokens: 20, Other: `{"cache_tokens":60}`}, output: 20, ambiguous: 1},
		{name: "malformed metadata", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":`}, output: 20, ambiguous: 1},
		{name: "negative cache", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":-1}`}, output: 20, ambiguous: 1},
		{name: "fractional cache", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":0.5}`}, output: 20, ambiguous: 1},
		{name: "oversized metadata integer", row: usageLogRow{PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":18446744073709551615}`}, output: 20, ambiguous: 1},
		{name: "negative prompt", row: usageLogRow{PromptTokens: -1, CompletionTokens: 20, Other: `{"cache_tokens":0}`}, output: 20, ambiguous: 1},
		{name: "negative completion", row: usageLogRow{PromptTokens: 100, CompletionTokens: -1}, ambiguous: 1},
		{name: "local estimate", row: usageLogRow{PromptTokens: 100, Other: `{"admin_info":{"usage_billing_path":"local"}}`}, input: 100, estimated: 1},
		{name: "upstream partial estimate", row: usageLogRow{PromptTokens: 100, Other: `{"admin_info":{"usage_billing_path":"billing-usage-gemini-estimated"}}`}, input: 100, estimated: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeUsageLog(tc.row)
			require.NoError(t, err)
			assert.Equal(t, tc.input, got.InputTokens)
			assert.Equal(t, tc.output, got.OutputTokens)
			assert.Equal(t, tc.input+tc.output, got.TotalTokens)
			assert.Equal(t, tc.read, got.CacheReadTokens)
			assert.Equal(t, tc.write, got.CacheWriteTokens)
			assert.Equal(t, tc.ambiguous, got.AmbiguousRequests)
			assert.Equal(t, tc.estimated, got.EstimatedRequests)
			assert.Equal(t, int64(1), got.CacheRecordedRequests+got.CacheUnknownRequests)
		})
	}
}

func TestUsageTotalsRejectOverflow(t *testing.T) {
	for _, row := range []usageLogRow{
		{PromptTokens: math.MaxInt64, CompletionTokens: 1},
		{PromptTokens: 1, Other: `{"claude":true,"cache_tokens":9223372036854775807}`},
		{Other: `{"cache_creation_tokens_5m":9223372036854775807,"cache_creation_tokens_1h":1}`},
	} {
		_, err := normalizeUsageLog(row)
		assert.ErrorIs(t, err, ErrUsageOverflow)
	}
	metrics := UsageMetrics{TotalTokens: math.MaxInt64}
	assert.ErrorIs(t, metrics.add(UsageMetrics{TotalTokens: 1}), ErrUsageOverflow)
	metrics = UsageMetrics{CacheReadTokens: common.GetPointer(int64(math.MaxInt64))}
	assert.ErrorIs(t, metrics.add(UsageMetrics{CacheReadTokens: common.GetPointer(int64(1))}), ErrUsageOverflow)
}

func TestUsageAnalysisQueryValidation(t *testing.T) {
	valid := UsageAnalysisQuery{StartTimestamp: 100, EndTimestamp: 100 + MaxUsageTimeRange, Granularity: "hour", TimezoneOffset: -540}
	require.NoError(t, valid.Validate())
	valid.ModelName = strings.Repeat("模", 256)
	valid.Username = strings.Repeat("人", 64)
	assert.NoError(t, valid.Validate(), "filter limits count characters rather than UTF-8 bytes")
	for _, q := range []UsageAnalysisQuery{
		{StartTimestamp: 0, EndTimestamp: 100, Granularity: "hour"},
		{StartTimestamp: 100, EndTimestamp: 99, Granularity: "hour"},
		{StartTimestamp: 100, EndTimestamp: 101 + MaxUsageTimeRange, Granularity: "hour"},
		{StartTimestamp: 1, EndTimestamp: math.MaxInt64, Granularity: "hour"},
		{StartTimestamp: 100, EndTimestamp: 200, Granularity: "week"},
		{StartTimestamp: 100, EndTimestamp: 200, Granularity: "hour", TimezoneOffset: 841},
		{StartTimestamp: 100, EndTimestamp: 200, Granularity: "hour", UserID: -1},
	} {
		require.Error(t, q.Validate())
	}
}

func openUsageAnalysisTestDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "usage.db"))
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
	}
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "usage_analysis_test_"}})
	require.NoError(t, err)
	require.False(t, db.Migrator().HasTable(&Log{}), "use an isolated test database without stale fixture tables")
	require.NoError(t, db.AutoMigrate(&Log{}))
	previousLogDB, previousType := LOG_DB, common.LogDatabaseType()
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseType(dialect))
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&Log{}))
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousType)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	var version string
	if dialect == "sqlite" {
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	} else {
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	}
	t.Logf("%s version: %s", dialect, version)
	return db
}

func TestUsageAnalysisDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openUsageAnalysisTestDB(t, dialect)
			start := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC).Unix() // Tokyo Oct 9 midnight.
			rows := []Log{
				{UserId: 1, Username: "alice", Type: LogTypeConsume, ModelName: "gpt-a", CreatedAt: start, PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":60}`},
				{UserId: 1, Username: "alice", Type: LogTypeConsume, ModelName: "claude-a", CreatedAt: start + 3600, PromptTokens: 10, CompletionTokens: 20, Other: `{"claude":true,"cache_tokens":60,"cache_creation_tokens":30}`},
				{UserId: 2, Username: "bob", Type: LogTypeConsume, ModelName: "gpt-b", CreatedAt: start + 1, PromptTokens: 300, CompletionTokens: 50, Other: `{}`},
				{UserId: 1, Username: "alice", Type: LogTypeError, ModelName: "gpt-a", CreatedAt: start, PromptTokens: 9000, CompletionTokens: 9000},
				{UserId: 1, Username: "alice", Type: LogTypeConsume, ModelName: "gpt-a", CreatedAt: start - 1, PromptTokens: 9000, CompletionTokens: 9000},
				{UserId: 1, Username: "alice", Type: LogTypeConsume, ModelName: "gpt-a", CreatedAt: start + 3601, PromptTokens: 9000, CompletionTokens: 9000},
			}
			require.NoError(t, db.Create(&rows).Error)
			q := UsageAnalysisQuery{StartTimestamp: start, EndTimestamp: start + 3600, Granularity: "hour", TimezoneOffset: -540, UserID: 1}
			got, err := GetUsageAnalysis(context.Background(), q)
			require.NoError(t, err)
			assert.Equal(t, int64(2), got.Summary.RequestCount)
			assert.Equal(t, int64(200), got.Summary.InputTokens)
			assert.Equal(t, int64(40), got.Summary.OutputTokens)
			assert.Equal(t, int64(240), got.Summary.TotalTokens)
			require.NotNil(t, got.Summary.CacheReadTokens)
			require.NotNil(t, got.Summary.CacheWriteTokens)
			require.NotNil(t, got.Summary.CacheRatio)
			assert.Equal(t, int64(120), *got.Summary.CacheReadTokens)
			assert.Equal(t, int64(30), *got.Summary.CacheWriteTokens)
			assert.InDelta(t, 0.6, *got.Summary.CacheRatio, 0.0001)
			require.Len(t, got.Models, 2)
			require.Len(t, got.Series, 2)
			assert.Equal(t, start, got.Series[0].CreatedAt)
			assert.Equal(t, start+3600, got.Series[1].CreatedAt)
			assert.Equal(t, "recorded", got.Meta.CacheSemantics)
			require.False(t, got.Meta.Incomplete)
			q.UserID = 0
			q.Username = "bob"
			got, err = GetUsageAnalysis(context.Background(), q)
			require.NoError(t, err)
			assert.Equal(t, int64(1), got.Summary.RequestCount)
			assert.Equal(t, int64(300), got.Summary.InputTokens)
			require.Nil(t, got.Summary.CacheReadTokens)
			require.Nil(t, got.Summary.CacheRatio)
			q.Username = "b%"
			got, err = GetUsageAnalysis(context.Background(), q)
			require.NoError(t, err)
			assert.Zero(t, got.Summary.RequestCount, "username filter is exact, not SQL LIKE")
			require.Nil(t, got.Summary.CacheReadTokens)
			assert.Empty(t, got.Models)
			require.Len(t, got.Series, 2, "empty periods still have complete zero-request buckets")
			q.Username = ""
			q.ModelName = "gpt-a"
			got, err = GetUsageAnalysis(context.Background(), q)
			require.NoError(t, err)
			assert.Equal(t, int64(1), got.Summary.RequestCount)
			q.ModelName = "gpt-%"
			got, err = GetUsageAnalysis(context.Background(), q)
			require.NoError(t, err)
			assert.Zero(t, got.Summary.RequestCount, "model filter is exact")
			q.ModelName = ""
			q.Granularity = "day"
			got, err = GetUsageAnalysis(context.Background(), q)
			require.NoError(t, err)
			require.Len(t, got.Series, 1)
			assert.Equal(t, start, got.Series[0].CreatedAt, "day bucket follows Tokyo midnight")
			assert.Equal(t, int64(3), got.Summary.RequestCount, "queries read the independent LOG_DB connection")
		})
	}
}

func TestUsageAnalysisAmbiguousAndEstimates(t *testing.T) {
	db := openUsageAnalysisTestDB(t, "sqlite")
	rows := []Log{
		{Type: LogTypeConsume, ModelName: "gpt", CreatedAt: 100, PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":0,"admin_info":{"local_count_tokens":true}}`},
		{Type: LogTypeConsume, ModelName: "gpt", CreatedAt: 100, PromptTokens: 1, CompletionTokens: 20, Other: `{"cache_tokens":60}`},
		{Type: LogTypeConsume, ModelName: "gpt", CreatedAt: 100, PromptTokens: 1, CompletionTokens: 20, Other: `not json`},
	}
	require.NoError(t, db.Create(&rows).Error)
	got, err := GetUsageAnalysis(context.Background(), UsageAnalysisQuery{StartTimestamp: 100, EndTimestamp: 100, Granularity: "hour"})
	require.NoError(t, err)
	assert.Equal(t, int64(100), got.Summary.InputTokens)
	assert.Equal(t, int64(60), got.Summary.OutputTokens)
	assert.Equal(t, int64(2), got.Summary.AmbiguousRequests)
	assert.Equal(t, int64(1), got.Summary.EstimatedRequests)
	assert.Equal(t, int64(1), got.Summary.CacheRecordedRequests)
	assert.Equal(t, int64(2), got.Summary.CacheUnknownRequests)
	require.NotNil(t, got.Summary.CacheReadTokens)
	require.NotNil(t, got.Summary.CacheRatio)
	assert.Zero(t, *got.Summary.CacheReadTokens, "excluded input also excludes the cache numerator")
	assert.Zero(t, *got.Summary.CacheRatio)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = GetUsageAnalysis(ctx, UsageAnalysisQuery{StartTimestamp: 100, EndTimestamp: 100, Granularity: "hour"})
	require.True(t, errors.Is(err, context.Canceled), err)
}

func TestUsageAnalysisOffsetBuckets(t *testing.T) {
	db := openUsageAnalysisTestDB(t, "sqlite")
	require.NoError(t, db.Create(&Log{Type: LogTypeConsume, CreatedAt: 1801, PromptTokens: 1, CompletionTokens: 2}).Error)
	for _, tc := range []struct {
		offset      int
		granularity string
		bucket      int64
	}{
		{offset: -330, granularity: "hour", bucket: 1800},
		{offset: 300, granularity: "day", bucket: -68400},
	} {
		got, err := GetUsageAnalysis(context.Background(), UsageAnalysisQuery{StartTimestamp: 1801, EndTimestamp: 1801, Granularity: tc.granularity, TimezoneOffset: tc.offset})
		require.NoError(t, err)
		require.Len(t, got.Series, 1)
		assert.Equal(t, tc.bucket, got.Series[0].CreatedAt)
		assert.Equal(t, int64(3), got.Series[0].TotalTokens)
	}
}

func TestUsageAnalysisRejectsExcessRows(t *testing.T) {
	db := openUsageAnalysisTestDB(t, "sqlite")
	// This small-column fixture exercises the real cursor limit without
	// allocating 250001 full Log objects or changing the production row limit.
	require.NoError(t, db.Exec(`WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n < ?)
		INSERT INTO usage_analysis_test_logs (type, created_at, model_name, prompt_tokens, completion_tokens, other)
		SELECT 2, 100, 'gpt', 1, 1, '{}' FROM numbers`, maxUsageLogRows+1).Error)
	got, err := GetUsageAnalysis(context.Background(), UsageAnalysisQuery{StartTimestamp: 100, EndTimestamp: 100, Granularity: "hour"})
	require.Nil(t, got, "never publish silently truncated analysis")
	assert.ErrorIs(t, err, ErrUsageTooLarge)
	require.NoError(t, db.Where("id = ?", maxUsageLogRows+1).Delete(&Log{}).Error)
	got, err = GetUsageAnalysis(context.Background(), UsageAnalysisQuery{StartTimestamp: 100, EndTimestamp: 100, Granularity: "hour"})
	require.NoError(t, err)
	assert.Equal(t, int64(maxUsageLogRows), got.Summary.RequestCount, "the exact row limit remains accepted")
}
