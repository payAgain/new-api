package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.FlowQuotaData `json:"data"`
}

func setupFlowControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.QuotaData{}))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "vip",
		ChannelID: 1,
		ModelName: "gpt-b",
		CreatedAt: 1200,
		Count:     1,
		Quota:     70,
		TokenUsed: 30,
	}).Error)
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "vip", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Empty(t, payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].NodeName)
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Empty(t, payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}

func TestUsageAnalysisControllerIsolationAndValidation(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "gpt-a", CreatedAt: 1000, PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":0,"admin_info":{"billing_model":"private-diagnostic"}}`},
		{UserId: 2, Username: "bob", Type: model.LogTypeConsume, ModelName: "gpt-b", CreatedAt: 1000, PromptTokens: 200, CompletionTokens: 30, Other: `{}`},
	}).Error)
	for _, tc := range []struct {
		name, path string
		self       bool
		id, role   int
		status     int
		count      int64
		success    bool
	}{
		{name: "self ignores identity query parameters", path: "/api/data/usage/self?start_timestamp=1000&end_timestamp=2000&username=bob&user_id=2", self: true, id: 1, role: common.RoleCommonUser, status: 200, count: 1, success: true},
		{name: "self retains model filter without widening identity", path: "/api/data/usage/self?start_timestamp=1000&end_timestamp=2000&username=bob&model_name=gpt-b", self: true, id: 1, role: common.RoleRootUser, status: 200, success: true},
		{name: "admin all logs", path: "/api/data/usage?start_timestamp=1000&end_timestamp=2000", id: 1, role: common.RoleAdminUser, status: 200, count: 2, success: true},
		{name: "admin username filter", path: "/api/data/usage?start_timestamp=1000&end_timestamp=2000&username=bob", id: 1, role: common.RoleAdminUser, status: 200, count: 1, success: true},
		{name: "common user cannot use admin handler", path: "/api/data/usage?start_timestamp=1000&end_timestamp=2000", id: 1, role: common.RoleCommonUser, status: 403},
		{name: "self requires identity", path: "/api/data/usage/self?start_timestamp=1000&end_timestamp=2000", self: true, status: 401},
		{name: "missing range", path: "/api/data/usage/self", self: true, id: 1, status: 200},
		{name: "malformed timestamp", path: "/api/data/usage/self?start_timestamp=bad&end_timestamp=2000", self: true, id: 1, status: 200},
		{name: "reversed range", path: "/api/data/usage/self?start_timestamp=2000&end_timestamp=1000", self: true, id: 1, status: 200},
		{name: "range exceeding a month retains self isolation", path: "/api/data/usage/self?start_timestamp=1&end_timestamp=2678402&username=bob", self: true, id: 1, status: 200, count: 1, success: true},
		{name: "invalid granularity", path: "/api/data/usage/self?start_timestamp=1000&end_timestamp=2000&granularity=week", self: true, id: 1, status: 200},
		{name: "invalid timezone", path: "/api/data/usage/self?start_timestamp=1000&end_timestamp=2000&timezone_offset=841", self: true, id: 1, status: 200},
		{name: "malformed timezone", path: "/api/data/usage/self?start_timestamp=1000&end_timestamp=2000&timezone_offset=oops", self: true, id: 1, status: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set("id", tc.id)
			ctx.Set("role", tc.role)
			ctx.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.self {
				GetSelfUsageAnalysis(ctx)
			} else {
				GetAllUsageAnalysis(ctx)
			}
			require.Equal(t, tc.status, recorder.Code)
			var response struct {
				Success bool                `json:"success"`
				Data    model.UsageAnalysis `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, tc.success, response.Success, recorder.Body.String())
			if tc.success {
				assert.Equal(t, tc.count, response.Data.Summary.RequestCount)
				assert.NotContains(t, recorder.Body.String(), "admin_info")
				assert.NotContains(t, recorder.Body.String(), "private-diagnostic")
			}
		})
	}
}
