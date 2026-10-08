package workbuddy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fixtureTransport struct {
	target string
	base   http.RoundTripper
}

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	next := r.Clone(r.Context())
	target, _ := http.NewRequest(r.Method, f.target+r.URL.RequestURI(), nil)
	next.URL = target.URL
	return f.base.RoundTrip(next)
}
func fixtureClient(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return Client{HTTP: &http.Client{Transport: fixtureTransport{server.URL, http.DefaultTransport}}}
}
func fixtureCredential() Credential {
	return Credential{AccessToken: "test-access", RefreshToken: "test-refresh", UID: "test-uid", Domain: "codebuddy.cn"}
}
func fixturePool(c Client, accounts ...Credential) *Pool {
	return &Pool{config: Config{Accounts: accounts, Distribution: "priority"}, client: c, cooldown: map[string]time.Time{}, checkinDay: map[int]string{}, checkinRetry: map[int]time.Time{}}
}

func TestAccountConfigurationAndRequestCompatibility(t *testing.T) {
	c := fixtureCredential()
	raw, err := common.Marshal(c)
	require.NoError(t, err)
	cfg, err := ParseConfig(string(raw))
	require.NoError(t, err)
	require.Len(t, cfg.Accounts, 1)
	for _, bad := range []string{`null`, `{}`, `{"accessToken":"test","refreshToken":"test","uid":"x\r\nInjected: yes"}`, `{"accounts":[{"accessToken":"a","refreshToken":"r","uid":"u","domain":"codebuddy.cn"},{"accessToken":"b","refreshToken":"r","uid":"u","domain":"workbuddy.ai"}]}`} {
		_, err = ParseConfig(bad)
		require.Error(t, err)
	}
	prepared, err := PrepareChat([]byte(`{"messages":[{"role":"developer","content":"instruction"}],"temperature":0,"stream":false,"stream_options":{"include_usage":true},"tool_choice":{"type":"function","function":{"name":"lookup"}}}`))
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, common.Unmarshal(prepared, &out))
	require.Equal(t, true, out["stream"])
	require.Equal(t, float64(0), out["temperature"])
	require.Equal(t, "lookup", out["tool_choice"])
	require.NotContains(t, out, "stream_options")
	require.Equal(t, "system", out["messages"].([]any)[0].(map[string]any)["role"])
	_, err = PrepareChat([]byte(`null`))
	require.Error(t, err)
}
func TestModelsRefreshAndSecretRedaction(t *testing.T) {
	c := fixtureCredential()
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, c.UID, r.Header.Get("X-User-Id"))
		switch r.URL.Path {
		case "/v2/enterprises/personal/models":
			require.Equal(t, "Bearer test-access", r.Header.Get("Authorization"))
			io.WriteString(w, `{"code":0,"data":{"models":[{"id":"valid","maxInputTokens":100,"maxOutputTokens":20},{"id":"disabled","disabled":true,"maxInputTokens":100,"maxOutputTokens":20},{"id":"not-cli","maxInputTokens":100,"maxOutputTokens":20}],"agents":[{"name":"cli","models":["valid","disabled"]}]}}`)
		case "/v3/config":
			require.Equal(t, "WorkBuddy/5.5.2", r.Header.Get("User-Agent"))
			io.WriteString(w, `{"code":0,"data":{"models":[{"id":"global","maxInputTokens":100,"maxOutputTokens":20}]}}`)
		case "/v2/plugin/auth/token/refresh":
			require.Empty(t, r.Header.Get("Authorization"))
			require.Equal(t, c.RefreshToken, r.Header.Get("X-Refresh-Token"))
			io.WriteString(w, `{"code":0,"data":{"accessToken":"rotated-access","expiresIn":3600}}`)
		default:
			w.WriteHeader(403)
			io.WriteString(w, `{"code":1,"message":"test-access test-refresh"}`)
		}
	})
	models, err := client.Models(context.Background(), c)
	require.NoError(t, err)
	require.Equal(t, []string{"valid"}, models)
	global := c
	global.Domain = "workbuddy.ai"
	models, err = client.Models(context.Background(), global)
	require.NoError(t, err)
	require.Equal(t, []string{"global"}, models)
	next, err := client.Refresh(context.Background(), c)
	require.NoError(t, err)
	require.Equal(t, "rotated-access", next.AccessToken)
	require.Equal(t, c.RefreshToken, next.RefreshToken)
	require.Greater(t, next.ExpiresAtMs, time.Now().UnixMilli())
	_, err = client.Credits(context.Background(), c)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "test-access")
	require.NotContains(t, err.Error(), "test-refresh")
	p := fixturePool(client, c)
	status, err := common.Marshal(p.Status())
	require.NoError(t, err)
	require.NotContains(t, string(status), c.AccessToken)
	require.NotContains(t, string(status), c.RefreshToken)
}
func TestChatFailoverCooldownAndExpiredSessionRefresh(t *testing.T) {
	first := fixtureCredential()
	second := first
	second.AccessToken = "second-access"
	calls := []string{}
	refreshes := 0
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "refresh") {
			refreshes++
			io.WriteString(w, `{"code":0,"data":{"accessToken":"rotated","expiresIn":3600}}`)
			return
		}
		token := r.Header.Get("Authorization")
		calls = append(calls, token)
		if token == "Bearer test-access" {
			w.WriteHeader(429)
			return
		}
		if token == "Bearer expired" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	p := fixturePool(client, first, second)
	for range 2 {
		response, err := p.Chat(context.Background(), "model", []byte(`{"messages":[]}`))
		require.NoError(t, err)
		require.Equal(t, 200, response.StatusCode)
		response.Body.Close()
	}
	require.Equal(t, []string{"Bearer test-access", "Bearer second-access", "Bearer second-access"}, calls)
	expired := first
	expired.AccessToken = "expired"
	p = fixturePool(client, expired)
	response, err := p.Chat(context.Background(), "model", []byte(`{"messages":[]}`))
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	response.Body.Close()
	require.Equal(t, 1, refreshes)
	require.Equal(t, "rotated", p.config.Accounts[0].AccessToken)
}
func TestNonStreamingAggregationPreservesToolsReasoningAndUsage(t *testing.T) {
	stream := ": keepalive\r\n\r\ndata: {\"id\":\"chat-test\",\"model\":\"model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\",\"reasoning_content\":\"reason\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\"}}]}}]}\r\n\r\n" +
		`data: {"choices":[{"index":0,"delta":{"content":"好","tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":7}}}` + "\n\ndata: [DONE]\n\n"
	response, err := AggregateResponse(&http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))})
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, common.DecodeJson(response.Body, &out))
	choice := out["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	require.Equal(t, "你好", message["content"])
	require.Equal(t, "reason", message["reasoning_content"])
	require.Equal(t, "tool_calls", choice["finish_reason"])
	tool := message["tool_calls"].([]any)[0].(map[string]any)
	require.Equal(t, "call-1", tool["id"])
	require.Equal(t, map[string]any{"name": "lookup", "arguments": "{}"}, tool["function"])
	require.Equal(t, map[string]any{"prompt_tokens": float64(12), "completion_tokens": float64(4), "total_tokens": float64(16), "prompt_tokens_details": map[string]any{"cached_tokens": float64(7)}}, out["usage"])
	for _, broken := range []string{`data: {"error":{"message":"secret"}}` + "\n\n", `data: {"choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\n", `data: {"choices":[]}`} {
		_, err = AggregateResponse(&http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(broken))})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}
func TestAutomaticCheckinSkipsDisabledAccountsAndClaimsOnlyOnce(t *testing.T) {
	c := fixtureCredential()
	disabled := c
	disabled.Enabled = new(false)
	claimed := false
	claims, statuses := 0, 0
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			statuses++
			fmt.Fprintf(w, `{"code":0,"data":{"active":true,"today_checked_in":%t}}`, claimed)
		case "/v2/billing/meter/daily-checkin":
			claims++
			claimed = true
			io.WriteString(w, `{"code":0,"data":{}}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	})
	p := fixturePool(client, c, disabled)
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	p.tick(context.Background(), now.Add(-time.Minute))
	require.Zero(t, statuses)
	p.tick(context.Background(), now)
	p.tick(context.Background(), now.Add(time.Minute))
	require.Equal(t, 1, claims)
	require.Equal(t, 2, statuses)
	require.Equal(t, "2026-10-08", p.checkinDay[0])
	require.Empty(t, p.checkinDay[1])
	// A fresh process rechecks upstream status but never duplicates the claim.
	fresh := fixturePool(client, c)
	fresh.tick(context.Background(), now)
	require.Equal(t, 1, claims)
	_, err := p.AccountAction(context.Background(), 1, "checkin")
	require.Error(t, err)
	_, err = p.AccountAction(context.Background(), 0, "unknown")
	require.Error(t, err)
	off := false
	require.NoError(t, p.Settings(nil, &off, nil, nil))
	p.tick(context.Background(), now.AddDate(0, 0, 1))
	require.Equal(t, 3, statuses)
	require.Error(t, p.Settings(nil, nil, nil, &off))
}
func TestCredentialPersistenceDatabaseMatrix(t *testing.T) {
	cases := []struct {
		name    string
		dialect gorm.Dialector
		version string
	}{{"SQLite", sqlite.Open(filepath.Join(t.TempDir(), "workbuddy.db")), "select sqlite_version()"}}
	if dsn := os.Getenv("WORKBUDDY_TEST_MYSQL_DSN"); dsn != "" {
		cases = append(cases, struct {
			name    string
			dialect gorm.Dialector
			version string
		}{"MySQL", mysql.Open(dsn), "select version()"})
	}
	if dsn := os.Getenv("WORKBUDDY_TEST_POSTGRES_DSN"); dsn != "" {
		cases = append(cases, struct {
			name    string
			dialect gorm.Dialector
			version string
		}{"PostgreSQL", postgres.Open(dsn), "select version()"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(tc.dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { sqlDB.Close() })
			var version string
			require.NoError(t, db.Raw(tc.version).Scan(&version).Error)
			t.Log(version)
			require.NoError(t, db.AutoMigrate(&model.Channel{}))
			channel := model.Channel{Type: constant.ChannelTypeWorkBuddy, Name: "WorkBuddy fixture", Key: "CaseSensitive-old", Models: "hy3", Group: "default", Status: 1, UsedQuota: 123}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() { db.Delete(&model.Channel{}, channel.Id) })
			require.NoError(t, db.AutoMigrate(&model.Channel{}))
			require.Error(t, model.SaveWorkBuddyCredentials(db, channel.Id, "casesensitive-old", "invalid"))
			require.NoError(t, model.SaveWorkBuddyCredentials(db, channel.Id, "CaseSensitive-old", "rotated"))
			require.Error(t, model.SaveWorkBuddyCredentials(db, channel.Id, "CaseSensitive-old", "stale"))
			var stored model.Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			require.Equal(t, "rotated", stored.Key)
			require.Equal(t, channel.Name, stored.Name)
			require.Equal(t, channel.Models, stored.Models)
			require.Equal(t, channel.Group, stored.Group)
			require.Equal(t, int64(123), stored.UsedQuota)
			require.NoError(t, db.Model(&stored).Update("type", constant.ChannelTypeOpenAI).Error)
			require.Error(t, model.SaveWorkBuddyCredentials(db, channel.Id, "rotated", "invalid"))
			require.NoError(t, db.Delete(&stored).Error)
			require.Error(t, model.SaveWorkBuddyCredentials(db, channel.Id, "rotated", "invalid"))
		})
	}
}

func TestConcurrentRequestsRefreshExpiredCredentialOnce(t *testing.T) {
	var refreshes atomic.Int32
	c := fixtureCredential()
	c.ExpiresAtMs = 1
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "refresh") {
			refreshes.Add(1)
			io.WriteString(w, `{"code":0,"data":{"accessToken":"fresh","expiresIn":3600}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	p := fixturePool(client, c)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			r, err := p.Chat(context.Background(), "model", []byte(`{"messages":[]}`))
			if r != nil {
				r.Body.Close()
			}
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), refreshes.Load())
}
func TestCheckinRetriesFailureAndRejectsUnconfirmedClaim(t *testing.T) {
	requests := 0
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(503) })
	p := fixturePool(client, fixtureCredential())
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	p.tick(context.Background(), now)
	require.Empty(t, p.checkinDay[0])
	require.Equal(t, 1, requests)
	p.tick(context.Background(), now.Add(14*time.Minute))
	require.Equal(t, 1, requests)
	p.tick(context.Background(), now.Add(15*time.Minute))
	require.Equal(t, 2, requests)
	unconfirmed := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "daily-checkin") {
			io.WriteString(w, `{"code":0,"data":{}}`)
		} else {
			io.WriteString(w, `{"code":0,"data":{"active":true,"today_checked_in":false}}`)
		}
	})
	_, err := unconfirmed.Checkin(context.Background(), fixtureCredential())
	require.ErrorContains(t, err, "not been confirmed")
}
