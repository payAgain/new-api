package router

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestRelayDiagnosticsDatabaseMatrix(t *testing.T) {
	t.Setenv("ERROR_DIAGNOSTICS_ENABLED", "true")
	cases := []struct {
		name         string
		dialect      gorm.Dialector
		databaseType common.DatabaseType
		version      string
	}{
		{"SQLite", sqlite.Open(filepath.Join(t.TempDir(), "diagnostics.db")), common.DatabaseTypeSQLite, "select sqlite_version()"},
	}
	if dsn := os.Getenv("DIAGNOSTICS_TEST_MYSQL_DSN"); dsn != "" {
		cases = append(cases, struct {
			name         string
			dialect      gorm.Dialector
			databaseType common.DatabaseType
			version      string
		}{"MySQL", mysql.Open(dsn), common.DatabaseTypeMySQL, "select version()"})
	}
	if dsn := os.Getenv("DIAGNOSTICS_TEST_POSTGRES_DSN"); dsn != "" {
		cases = append(cases, struct {
			name         string
			dialect      gorm.Dialector
			databaseType common.DatabaseType
			version      string
		}{"PostgreSQL", postgres.Open(dsn), common.DatabaseTypePostgreSQL, "select version()"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(tc.dialect, &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			var version string
			require.NoError(t, db.Raw(tc.version).Scan(&version).Error)
			t.Logf("database: %s", version)
			previousDB, previousLogDB := model.DB, model.LOG_DB
			previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
			previousCache, previousRedis, previousErrorLogs := common.MemoryCacheEnabled, common.RedisEnabled, constant.ErrorLogEnabled
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetMainDatabaseType(previousMain)
				common.SetLogDatabaseType(previousLog)
				common.MemoryCacheEnabled, common.RedisEnabled, constant.ErrorLogEnabled = previousCache, previousRedis, previousErrorLogs
			})
			model.DB, model.LOG_DB = db, db
			common.SetMainDatabaseType(tc.databaseType)
			common.SetLogDatabaseType(tc.databaseType)
			common.MemoryCacheEnabled, common.RedisEnabled, constant.ErrorLogEnabled = false, false, true
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
			require.NoError(t, db.Create(&model.User{Id: 1, Username: "diagnostic-owner", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
			t.Cleanup(func() { db.Where("user_id = ?", 1).Delete(&model.Log{}); db.Delete(&model.User{}, 1) })
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, readErr := io.ReadAll(r.Body)
				require.NoError(t, readErr)
				assert.Equal(t, `{"messages":[{"role":"user","content":"forwarded fixture"}]}`, string(data))
				w.Header().Set("X-Request-Id", "upstream-fixture-id")
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(400)
				_, _ = w.Write([]byte("<html>unsupported fixture field messages</html>"))
			}))
			t.Cleanup(upstream.Close)
			engine := gin.New()
			engine.Use(middleware.RequestId(), middleware.RelayDiagnostics(), middleware.RouteTag("relay"), func(c *gin.Context) {
				c.Set("id", 1)
				c.Set("username", "diagnostic-owner")
				c.Set("original_model", "fixture-model")
				c.Set("group", "default")
				c.Next()
			})
			engine.POST("/upstream", func(c *gin.Context) {
				original, readErr := io.ReadAll(c.Request.Body)
				require.NoError(t, readErr)
				assert.Equal(t, "original fixture prompt", string(original))
				c.Set("channel_id", 11)
				diagnostics := common.GetRelayDiagnostics(c)
				require.NotNil(t, diagnostics)
				diagnostics.BeginAttempt(11, "fixture channel", "upstream-model", "openai_responses")
				req, reqErr := http.NewRequest(http.MethodPost, upstream.URL+"/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"forwarded fixture"}]}`))
				require.NoError(t, reqErr)
				req.Header.Set("Authorization", "Bearer fixture-upstream-secret")
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 11}, OriginModelName: "fixture-model", RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI}}
				resp, requestErr := channel.DoRequest(c, req, info)
				require.NoError(t, requestErr)
				apiErr := service.RelayErrorHandler(c.Request.Context(), resp, false)
				service.ResetStatusCode(apiErr, `{"400":502}`)
				service.ProcessChannelError(c, types.ChannelError{ChannelId: 11}, apiErr, info)
				service.RecordRelayFailure(c, apiErr, info)
				c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
			})
			engine.POST("/local", func(c *gin.Context) { controller.Relay(c, types.RelayFormatOpenAI) })
			engine.POST("/routing", func(c *gin.Context) {
				_, _ = io.ReadAll(c.Request.Body)
				c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"message": "fixture has no usable channels"}})
			})
			engine.POST("/success", func(c *gin.Context) { _, _ = io.ReadAll(c.Request.Body); c.JSON(200, gin.H{"ok": true}) })
			engine.GET("/own-logs", controller.GetUserLogs)
			for _, input := range []struct {
				path, body string
				status     int
			}{{"/upstream", "original fixture prompt", 502}, {"/local", `{"model":`, 400}, {"/routing", "routing fixture", 503}, {"/success", "successful fixture", 200}} {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, input.path, strings.NewReader(input.body))
				request.Header.Set("Authorization", "Bearer fixture-client-secret")
				request.Header.Set("Content-Type", "application/json")
				engine.ServeHTTP(response, request)
				assert.Equal(t, input.status, response.Code)
			}
			var logs []model.Log
			require.NoError(t, db.Order("created_at, request_id").Find(&logs).Error)
			require.Len(t, logs, 3, "one record per failure; successful body is not persisted and final relay error is not duplicated")
			for _, log := range logs {
				var stored struct {
					RootInfo struct {
						Diagnostics common.RelayDiagnosticSnapshot `json:"error_diagnostics"`
					} `json:"root_info"`
				}
				require.NoError(t, common.UnmarshalJsonStr(log.Other, &stored))
				d := stored.RootInfo.Diagnostics
				assert.Equal(t, 1, d.Version)
				assert.NotEmpty(t, log.RequestId)
				assert.Equal(t, log.RequestId, d.RequestID)
				assert.Equal(t, "Bearer fixture-client-secret", d.ClientRequest.Headers.Get("Authorization"))
				switch d.ClientRequest.URL {
				case "/upstream":
					require.Len(t, d.Attempts, 1)
					assert.Equal(t, 400, d.Attempts[0].Response.Status)
					assert.Equal(t, 502, d.Status)
					assert.Contains(t, d.Attempts[0].Response.Body.Data, "unsupported fixture field messages")
					assert.Contains(t, d.Attempts[0].Request.Body.Data, "forwarded fixture")
					assert.Equal(t, "original fixture prompt", d.ClientRequest.Body.Data)
					assert.Equal(t, "upstream-fixture-id", log.UpstreamRequestId)
					assert.Equal(t, "Bearer fixture-upstream-secret", d.Attempts[0].Request.Headers.Get("Authorization"))
				case "/local":
					assert.Equal(t, "validation", d.Stage)
					assert.Equal(t, `{"model":`, d.ClientRequest.Body.Data)
					assert.Empty(t, d.Attempts)
				case "/routing":
					assert.Contains(t, d.Error, "fixture has no usable channels")
				default:
					t.Fatalf("unexpected diagnostic path %s", d.ClientRequest.URL)
				}
				adminCopy := log
				model.FormatAdminLogs([]*model.Log{&adminCopy})
				assert.NotContains(t, adminCopy.Other, "fixture-client-secret")
				assert.NotContains(t, adminCopy.Other, "error_diagnostics")
				rootCopy := log
				model.FormatRootLogs([]*model.Log{&rootCopy})
				assert.Contains(t, rootCopy.Other, "fixture-client-secret")
			}
			ownLogs := httptest.NewRecorder()
			engine.ServeHTTP(ownLogs, httptest.NewRequest(http.MethodGet, "/own-logs", nil))
			require.Equal(t, http.StatusOK, ownLogs.Code)
			assert.Contains(t, ownLogs.Body.String(), "\"success\":true")
			assert.NotContains(t, ownLogs.Body.String(), "error_diagnostics")
			assert.NotContains(t, ownLogs.Body.String(), "fixture-client-secret")
		})
	}
}

func TestRelayDiagnosticsTransportStreamAndBounds(t *testing.T) {
	t.Setenv("ERROR_DIAGNOSTICS_ENABLED", "true")
	t.Run("transport cause remains available after public error replacement", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("fixture"))
		d := common.EnableRelayDiagnostics(c)
		d.BeginAttempt(11, "fixture", "model", "openai")
		server := httptest.NewServer(http.NotFoundHandler())
		target := server.URL
		server.Close()
		req, err := http.NewRequest(http.MethodPost, target, strings.NewReader("forwarded"))
		require.NoError(t, err)
		_, err = channel.DoRequest(c, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
		require.Error(t, err)
		var apiErr *types.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "upstream error: do request failed", apiErr.Error())
		snapshot := d.Snapshot(apiErr.Error(), "do_request_failed", "new_api_error", 500)
		require.Len(t, snapshot.Attempts, 1)
		assert.Contains(t, snapshot.Attempts[0].Error, "connect: connection refused")
		assert.Contains(t, snapshot.Attempts[0].Error, target)
	})
	t.Run("stream read failure and original frames survive consume log projection", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		d := common.EnableRelayDiagnostics(c)
		d.BeginAttempt(11, "fixture", "model", "openai_responses")
		response := &http.Response{StatusCode: 200, Body: io.NopCloser(&diagnosticBrokenReader{})}
		d.CaptureResponse(response, nil)
		data, err := io.ReadAll(response.Body)
		assert.Equal(t, "data: fixture partial\n", string(data))
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
		info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}, StartTime: time.Now(), StreamStatus: relaycommon.NewStreamStatus()}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
		assert.Contains(t, other.JSONString(), "error_diagnostics")
		assert.Contains(t, other.JSONString(), "unexpected EOF")
		assert.Contains(t, other.JSONString(), "fixture partial")
	})
	t.Run("binary and bounded payloads preserve the original request reader", func(t *testing.T) {
		t.Setenv("ERROR_DIAGNOSTICS_MAX_BODY_KB", "1")
		t.Setenv("ERROR_DIAGNOSTICS_MAX_TOTAL_KB", "1")
		t.Setenv("ERROR_DIAGNOSTICS_MAX_ATTEMPTS", "1")
		data := bytes.Repeat([]byte{0xff}, 2048)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(data))
		d := common.EnableRelayDiagnostics(c)
		read, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		assert.Equal(t, data, read)
		d.BeginAttempt(1, "first", "model", "openai")
		d.BeginAttempt(2, "omitted", "model", "openai")
		snapshot := d.Snapshot("fixture", "", "", 400)
		assert.True(t, snapshot.ClientRequest.Body.Truncated)
		assert.Equal(t, "base64", snapshot.ClientRequest.Body.Encoding)
		assert.Equal(t, int64(2048), snapshot.ClientRequest.Body.Bytes)
		assert.Equal(t, 1024, snapshot.ClientRequest.Body.CapturedBytes)
		assert.Equal(t, 1, snapshot.OmittedAttempts)
		require.Len(t, snapshot.Attempts, 1)
		assert.Equal(t, 1, snapshot.Attempts[0].ChannelID)
	})
}

type diagnosticBrokenReader struct{ read bool }

func (r *diagnosticBrokenReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(p, []byte("data: fixture partial\n")), io.ErrUnexpectedEOF
}
