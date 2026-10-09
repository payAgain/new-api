package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// DecideRelayRetry is the single retry decision for relay attempts. The reason
// is recorded in the request policy decision events of the log details.
func DecideRelayRetry(c *gin.Context, err *types.NewAPIError, retryTimes int) PolicyDecision {
	if err == nil {
		return PolicyDecision{Action: "stop", Reason: "request_completed", Source: "system"}
	}
	if ShouldSkipRetryAfterChannelAffinityFailure(c) {
		source := RequestPolicy(c).SessionModeSource
		if source == "" {
			source = "session_rule"
		}
		return PolicyDecision{Action: "stop", Reason: "strict_session", Source: source}
	}
	if GetChannelConstraints(c).SuppressesRetry() {
		return PolicyDecision{Action: "stop", Reason: "pinned_channel", Source: "channel_constraint"}
	}
	if types.IsChannelError(err) {
		return PolicyDecision{Action: "retry", Reason: "channel_error", Source: "system"}
	}
	if types.IsSkipRetryError(err) {
		return PolicyDecision{Action: "stop", Reason: "non_retryable_error", Source: "system"}
	}
	if retryTimes <= 0 {
		return PolicyDecision{Action: "stop", Reason: "attempt_budget_exhausted", Source: "global"}
	}
	code := err.StatusCode
	if code >= 200 && code < 300 {
		return PolicyDecision{Action: "stop", Reason: "system_retry_exclusion", Source: "system"}
	}
	if code < 100 || code > 599 {
		return PolicyDecision{Action: "retry", Reason: "unrecognized_status", Source: "system"}
	}
	if operation_setting.IsAlwaysSkipRetryCode(err.GetErrorCode()) || operation_setting.IsAlwaysSkipRetryStatusCode(code) {
		return PolicyDecision{Action: "stop", Reason: "system_retry_exclusion", Source: "system"}
	}
	if operation_setting.ShouldRetryByStatusCode(code) {
		return PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"}
	}
	return PolicyDecision{Action: "stop", Reason: "status_not_retryable", Source: "global"}
}

func ShouldRetryRelayError(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	return DecideRelayRetry(c, openaiErr, retryTimes).Action == "retry"
}

func ProcessChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, relayInfo *relaycommon.RelayInfo) {
	if err == nil {
		return
	}
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.MaskSensitiveErrorWithStatusCode())))
	if ShouldDisableChannel(err) && channelError.AutoBan {
		reason := err.MaskSensitiveErrorWithStatusCode()
		gopool.Go(func() {
			DisableChannel(channelError, reason)
		})
	}

	if types.IsRecordErrorLog(err) {
		recordRelayFailure(c, err, relayInfo, channelError.ChannelId)
	}
}

const recordedRelayErrorKey = "recorded_relay_error"

// RecordRelayFailure also covers validation, billing and routing failures that
// never reach a channel attempt. A final handler can call it without duplicating
// the same error already recorded by ProcessChannelError.
func RecordRelayFailure(c *gin.Context, err *types.NewAPIError, relayInfo *relaycommon.RelayInfo) {
	recordRelayFailure(c, err, relayInfo, c.GetInt("channel_id"))
}

func recordRelayFailure(c *gin.Context, err *types.NewAPIError, relayInfo *relaycommon.RelayInfo, channelID int) {
	if err == nil || !constant.ErrorLogEnabled {
		return
	}
	if recorded, _ := c.Get(recordedRelayErrorKey); recorded == err {
		return
	}
	other := model.NewLogOther()
	appendRequestPath(c, relayInfo, other)
	other.SetPublic("error_type", err.GetErrorType())
	other.SetPublic("error_code", err.GetErrorCode())
	other.SetPublic("status_code", err.StatusCode)
	AppendRelayLogAdminInfo(c, relayInfo, other)
	AppendResponseModelLogInfo(relayInfo, other)
	AppendTaskPluginContextAuditInfo(c, other)
	appendRequestConversionChain(relayInfo, other)
	appendFinalRequestFormat(relayInfo, other)
	appendParamOverrideInfo(relayInfo, other)
	appendStreamStatus(c, relayInfo, other)
	if relayInfo != nil && relayInfo.ChannelMeta != nil && relayInfo.IsModelMapped {
		other.SetPublic("is_model_mapped", true)
		other.SetPublic("upstream_model_name", relayInfo.UpstreamModelName)
	}
	if diagnostics := common.GetRelayDiagnostics(c); diagnostics != nil {
		diagnostics.FinishAttempt(err.Error())
		other.SetRoot("error_diagnostics", diagnostics.Snapshot(err.Error(), string(err.GetErrorCode()), string(err.GetErrorType()), err.StatusCode))
	}
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	model.RecordErrorLog(c, c.GetInt("id"), channelID, c.GetString("original_model"), c.GetString("token_name"), err.MaskSensitiveErrorWithStatusCode(), c.GetInt("token_id"), int(time.Since(startTime).Seconds()), common.GetContextKeyBool(c, constant.ContextKeyIsStream), c.GetString("group"), other)
	c.Set(recordedRelayErrorKey, err)
}

func HasRecordedRelayFailure(c *gin.Context) bool {
	_, exists := c.Get(recordedRelayErrorKey)
	return exists
}
