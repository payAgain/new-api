package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	MaxUsageTimeRange = int64(31 * 24 * 60 * 60)
	maxUsageLogRows   = 250000
)

var (
	ErrUsageTimeRange = errors.New("invalid usage time range (maximum 31 days)")
	ErrUsageOptions   = errors.New("invalid usage query options")
	ErrUsageTooLarge  = errors.New("too many usage records; select a shorter time range")
	ErrUsageOverflow  = errors.New("usage totals exceed the supported range")
)

// UsageMetrics counts recorded usage, not prices or inferred cache savings.
// A nil cache value means no valid cache quantity was recorded for the group.
type UsageMetrics struct {
	RequestCount          int64    `json:"request_count"`
	InputTokens           int64    `json:"input_tokens"`
	OutputTokens          int64    `json:"output_tokens"`
	TotalTokens           int64    `json:"total_tokens"`
	CacheReadTokens       *int64   `json:"cache_read_tokens"`
	CacheWriteTokens      *int64   `json:"cache_write_tokens"`
	CacheRatio            *float64 `json:"cache_ratio"`
	CacheRecordedRequests int64    `json:"cache_recorded_requests"`
	CacheUnknownRequests  int64    `json:"cache_unknown_requests"`
	EstimatedRequests     int64    `json:"estimated_requests"`
	AmbiguousRequests     int64    `json:"ambiguous_requests"`
}

type ModelUsage struct {
	ModelName string `json:"model_name"`
	UsageMetrics
}

type UsageBucket struct {
	CreatedAt int64 `json:"created_at"`
	UsageMetrics
}

type UsageAnalysisMeta struct {
	StartTimestamp int64  `json:"start_timestamp"`
	EndTimestamp   int64  `json:"end_timestamp"`
	Granularity    string `json:"granularity"`
	TimezoneOffset int    `json:"timezone_offset"`
	CacheSemantics string `json:"cache_semantics"`
	Incomplete     bool   `json:"incomplete"`
	Availability   string `json:"availability"`
}

type UsageAnalysis struct {
	Summary UsageMetrics      `json:"summary"`
	Models  []ModelUsage      `json:"models"`
	Series  []UsageBucket     `json:"series"`
	Meta    UsageAnalysisMeta `json:"meta"`
}

type UsageAnalysisQuery struct {
	StartTimestamp int64
	EndTimestamp   int64
	Granularity    string
	// TimezoneOffset follows JavaScript Date.getTimezoneOffset(): Tokyo is -540.
	TimezoneOffset int
	UserID         int
	Username       string
	ModelName      string
}

func (q UsageAnalysisQuery) Validate() error {
	if q.StartTimestamp <= 0 || q.EndTimestamp < q.StartTimestamp || q.EndTimestamp > 253402300799 || q.EndTimestamp-q.StartTimestamp > MaxUsageTimeRange {
		return ErrUsageTimeRange
	}
	if q.UserID < 0 || utf8.RuneCountInString(q.Username) > 64 || utf8.RuneCountInString(q.ModelName) > 256 || q.TimezoneOffset < -840 || q.TimezoneOffset > 840 || q.Granularity != "hour" && q.Granularity != "day" {
		return ErrUsageOptions
	}
	return nil
}

type usageLogRow struct {
	CreatedAt        int64
	ModelName        string
	PromptTokens     int64
	CompletionTokens int64
	Other            string
}

type usageLogMetadata struct {
	InputTokensTotal    *int64 `json:"input_tokens_total"`
	CacheTokens         *int64 `json:"cache_tokens"`
	CacheWriteTokens    *int64 `json:"cache_write_tokens"`
	CacheCreationTokens *int64 `json:"cache_creation_tokens"`
	CacheCreation5m     *int64 `json:"cache_creation_tokens_5m"`
	CacheCreation1h     *int64 `json:"cache_creation_tokens_1h"`
	UsageSemantic       string `json:"usage_semantic"`
	Claude              bool   `json:"claude"`
	AdminInfo           struct {
		UsageBillingPath string `json:"usage_billing_path"`
		LocalCountTokens bool   `json:"local_count_tokens"`
	} `json:"admin_info"`
}

// normalizeUsageLog preserves Anthropic's exclusive input semantics while
// keeping OpenAI/Gemini cache quantities inside their inclusive input total.
// Historical cache zeroes mean "recorded zero", never "upstream reported zero".
func normalizeUsageLog(row usageLogRow) (UsageMetrics, error) {
	metrics := UsageMetrics{RequestCount: 1}
	metadata := usageLogMetadata{}
	ambiguous := row.PromptTokens < 0 || row.CompletionTokens < 0
	if row.Other != "" && common.UnmarshalJsonStr(row.Other, &metadata) != nil {
		metadata = usageLogMetadata{}
		ambiguous = true
	}
	metrics.OutputTokens = max(row.CompletionTokens, 0)
	for _, quantity := range []*int64{metadata.InputTokensTotal, metadata.CacheTokens, metadata.CacheWriteTokens, metadata.CacheCreationTokens, metadata.CacheCreation5m, metadata.CacheCreation1h} {
		if quantity != nil && *quantity < 0 {
			ambiguous = true
		}
	}
	if metadata.AdminInfo.LocalCountTokens || metadata.AdminInfo.UsageBillingPath == "local" || strings.HasSuffix(metadata.AdminInfo.UsageBillingPath, "-estimated") {
		metrics.EstimatedRequests = 1
	}
	if !ambiguous {
		if metadata.CacheTokens != nil {
			metrics.CacheReadTokens = common.GetPointer(*metadata.CacheTokens)
		}
		var writeTotal int64
		hasWrite := false
		for _, quantity := range []*int64{metadata.CacheWriteTokens, metadata.CacheCreationTokens} {
			if quantity != nil {
				hasWrite = true
				writeTotal = max(writeTotal, *quantity)
			}
		}
		var splitWrite int64
		for _, quantity := range []*int64{metadata.CacheCreation5m, metadata.CacheCreation1h} {
			if quantity != nil {
				hasWrite = true
				if *quantity > math.MaxInt64-splitWrite {
					return metrics, ErrUsageOverflow
				}
				splitWrite += *quantity
			}
		}
		writeTotal = max(writeTotal, splitWrite)
		if hasWrite {
			metrics.CacheWriteTokens = common.GetPointer(writeTotal)
		}
		anthropic := metadata.UsageSemantic == "anthropic" || metadata.Claude
		if metadata.InputTokensTotal != nil {
			metrics.InputTokens = *metadata.InputTokensTotal
		} else if anthropic {
			metrics.InputTokens = row.PromptTokens
			for _, quantity := range []*int64{metrics.CacheReadTokens, metrics.CacheWriteTokens} {
				if quantity != nil {
					if *quantity > math.MaxInt64-metrics.InputTokens {
						return metrics, ErrUsageOverflow
					}
					metrics.InputTokens += *quantity
				}
			}
		} else if metadata.UsageSemantic == "" && (metadata.CacheCreation5m != nil || metadata.CacheCreation1h != nil) {
			// Older converted Claude records lack an explicit inclusive total.
			ambiguous = true
		} else {
			metrics.InputTokens = row.PromptTokens
		}
	}
	if metrics.CacheReadTokens != nil && *metrics.CacheReadTokens > metrics.InputTokens {
		ambiguous = true
	}
	if ambiguous {
		metrics.InputTokens = 0
		metrics.AmbiguousRequests = 1
		metrics.CacheReadTokens = nil
		metrics.CacheWriteTokens = nil
	}
	if metrics.CacheReadTokens != nil || metrics.CacheWriteTokens != nil {
		metrics.CacheRecordedRequests = 1
	} else {
		metrics.CacheUnknownRequests = 1
	}
	if metrics.OutputTokens > math.MaxInt64-metrics.InputTokens {
		return metrics, ErrUsageOverflow
	}
	metrics.TotalTokens = metrics.InputTokens + metrics.OutputTokens
	return metrics, nil
}

func (m *UsageMetrics) add(value UsageMetrics) error {
	for _, count := range []struct{ target, incoming *int64 }{
		{&m.RequestCount, &value.RequestCount}, {&m.InputTokens, &value.InputTokens},
		{&m.OutputTokens, &value.OutputTokens}, {&m.TotalTokens, &value.TotalTokens},
		{&m.CacheRecordedRequests, &value.CacheRecordedRequests}, {&m.CacheUnknownRequests, &value.CacheUnknownRequests},
		{&m.EstimatedRequests, &value.EstimatedRequests}, {&m.AmbiguousRequests, &value.AmbiguousRequests},
	} {
		if *count.incoming < 0 || *count.incoming > math.MaxInt64-*count.target {
			return ErrUsageOverflow
		}
		*count.target += *count.incoming
	}
	for _, count := range []struct {
		target   **int64
		incoming *int64
	}{
		{&m.CacheReadTokens, value.CacheReadTokens}, {&m.CacheWriteTokens, value.CacheWriteTokens},
	} {
		if count.incoming == nil {
			continue
		}
		if *count.target == nil {
			*count.target = common.GetPointer(int64(0))
		}
		if *count.incoming < 0 || *count.incoming > math.MaxInt64-**count.target {
			return ErrUsageOverflow
		}
		**count.target += *count.incoming
	}
	return nil
}

func (m *UsageMetrics) recordedCacheRatio() {
	if m.CacheReadTokens != nil && m.InputTokens > 0 {
		ratio := float64(*m.CacheReadTokens) / float64(m.InputTokens)
		m.CacheRatio = &ratio
	}
}

// GetUsageAnalysis reads the log connection, including a separately configured
// log database, without changing settlement, log writers or database schemas.
func GetUsageAnalysis(ctx context.Context, q UsageAnalysisQuery) (*UsageAnalysis, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	query := LOG_DB.WithContext(ctx).Model(&Log{}).
		Select("created_at, model_name, prompt_tokens, completion_tokens, other").
		Where("type = ? AND created_at >= ? AND created_at <= ?", LogTypeConsume, q.StartTimestamp, q.EndTimestamp).
		Limit(maxUsageLogRows + 1)
	if q.UserID > 0 {
		query = query.Where("user_id = ?", q.UserID)
	}
	if q.Username != "" {
		query = query.Where("username = ?", q.Username)
	}
	if q.ModelName != "" {
		query = query.Where("model_name = ?", q.ModelName)
	}
	rows, err := query.Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &UsageAnalysis{
		Models: []ModelUsage{}, Series: []UsageBucket{},
		Meta: UsageAnalysisMeta{q.StartTimestamp, q.EndTimestamp, q.Granularity, q.TimezoneOffset, "recorded", false, "logs"},
	}
	models := map[string]*ModelUsage{}
	buckets := map[int64]*UsageBucket{}
	bucketSize := int64(3600)
	if q.Granularity == "day" {
		bucketSize = 86400
	}
	offsetSeconds := int64(q.TimezoneOffset) * 60
	localStart := q.StartTimestamp - offsetSeconds
	firstBucket := localStart - ((localStart%bucketSize + bucketSize) % bucketSize) + offsetSeconds
	for timestamp := firstBucket; timestamp <= q.EndTimestamp; timestamp += bucketSize {
		buckets[timestamp] = &UsageBucket{CreatedAt: timestamp}
	}
	for rows.Next() {
		if result.Summary.RequestCount >= maxUsageLogRows {
			return nil, ErrUsageTooLarge
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var createdAt, prompt, completion sql.NullInt64
		var name, other sql.NullString
		if err := rows.Scan(&createdAt, &name, &prompt, &completion, &other); err != nil {
			return nil, err
		}
		value, err := normalizeUsageLog(usageLogRow{createdAt.Int64, name.String, prompt.Int64, completion.Int64, other.String})
		if err != nil {
			return nil, err
		}
		modelUsage := models[name.String]
		if modelUsage == nil {
			modelUsage = &ModelUsage{ModelName: name.String}
			models[name.String] = modelUsage
		}
		localTimestamp := createdAt.Int64 - offsetSeconds
		// Normalize modulo for timestamps near the epoch in western timezones.
		bucketTimestamp := localTimestamp - ((localTimestamp%bucketSize + bucketSize) % bucketSize) + offsetSeconds
		bucket := buckets[bucketTimestamp]
		if bucket == nil {
			bucket = &UsageBucket{CreatedAt: bucketTimestamp}
			buckets[bucketTimestamp] = bucket
		}
		for _, metrics := range []*UsageMetrics{&result.Summary, &modelUsage.UsageMetrics, &bucket.UsageMetrics} {
			if err := metrics.add(value); err != nil {
				return nil, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.Summary.recordedCacheRatio()
	for _, value := range models {
		value.recordedCacheRatio()
		result.Models = append(result.Models, *value)
	}
	for _, value := range buckets {
		value.recordedCacheRatio()
		result.Series = append(result.Series, *value)
	}
	slices.SortFunc(result.Models, func(a, b ModelUsage) int {
		if a.TotalTokens > b.TotalTokens {
			return -1
		}
		if a.TotalTokens < b.TotalTokens {
			return 1
		}
		return strings.Compare(a.ModelName, b.ModelName)
	})
	slices.SortFunc(result.Series, func(a, b UsageBucket) int {
		if a.CreatedAt < b.CreatedAt {
			return -1
		}
		if a.CreatedAt > b.CreatedAt {
			return 1
		}
		return 0
	})
	return result, nil
}
