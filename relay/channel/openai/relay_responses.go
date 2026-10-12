package openai

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if strings.EqualFold(contentType, "text/event-stream") {
		return OaiResponsesBufferedStreamHandler(c, info, resp)
	}
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	info.ObserveResponseModel(responsesResponse.Model)
	responseBody = rewriteSGLangResponsesCreatedAt(info, responseBody, "created_at", responsesResponse.CreatedAt)

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := &dto.Usage{}
	service.ApplyResponsesUsage(usage, responsesResponse.Usage)
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}
	info.ApplyVendorToolUsage(responseBody)

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	for i := range responsesResponse.Output {
		imageCounter.Observe(&responsesResponse.Output[i], &i)
	}
	imageCounter.Commit(info)

	return usage, nil
}

// OaiResponsesBufferedStreamHandler accepts a streaming upstream without
// changing a non-streaming client's protocol. Only an explicit terminal
// response can become JSON; EOF or [DONE] alone must not manufacture success.
func OaiResponsesBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	var finalBody string
	var streamErr *types.NewAPIError
	helper.BufferedStreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var event dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			sr.Stop(streamErr)
			return
		}
		service.ObserveResponsesOutcome(info, &event)
		switch {
		case info.StreamStatus.ResponseFailed() || info.StreamStatus.ResponseOutcome() == string(relaycommon.ResponseOutcomeCancelled):
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream ended with %s", event.Type), types.ErrorCodeBadResponse, http.StatusBadGateway)
			if event.Response != nil {
				if upstreamErr := event.Response.GetOpenAIError(); upstreamErr != nil && upstreamErr.Type != "" {
					streamErr = types.WithOpenAIError(*upstreamErr, http.StatusBadGateway)
				}
			}
			sr.Stop(streamErr)
		case event.Type == "response.completed" || event.Type == "response.done" || event.Type == "response.incomplete":
			if event.Response == nil {
				streamErr = types.NewOpenAIError(fmt.Errorf("terminal responses event is missing response"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				sr.Stop(streamErr)
				return
			}
			// Retain fields unknown to the DTO, including provider extensions.
			finalBody = gjson.Get(data, "response").Raw
			sr.Done()
		}
	})
	common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, info.StreamStatus)
	info.StreamStatus.RequireTerminal()
	if streamErr != nil {
		return nil, streamErr
	}
	if finalBody == "" {
		return nil, types.NewOpenAIError(fmt.Errorf("responses stream ended without a terminal response: %s", info.StreamStatus.Summary()), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	// Reuse the JSON handler so usage and billable output calls are processed
	// exactly once, under the same rules as a native non-streaming response.
	jsonResponse := *resp
	jsonResponse.Header = resp.Header.Clone()
	jsonResponse.Header.Set("Content-Type", "application/json")
	jsonResponse.Header.Del("Content-Length")
	jsonResponse.Header.Del("Content-Encoding")
	jsonResponse.Body = io.NopCloser(strings.NewReader(finalBody))
	return OaiResponsesHandler(c, info, &jsonResponse)
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, common.LogText("invalid response or response body"))
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	accumulator := service.NewResponsesUsageAccumulator(info)

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal stream response: %s", err.Error()))
			sr.Error(err)
			return
		}
		if streamResponse.Response != nil {
			data = string(rewriteSGLangResponsesCreatedAt(info, []byte(data), "response.created_at", streamResponse.Response.CreatedAt))
		}
		sendResponsesStreamData(c, streamResponse, data)
		accumulator.Observe(&streamResponse, common.StringToByteSlice(data))
	})

	common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, info.StreamStatus)
	info.StreamStatus.RequireTerminal()
	return accumulator.Finish(), nil
}

func rewriteSGLangResponsesCreatedAt(info *relaycommon.RelayInfo, payload []byte, path string, createdAt dto.IntValue) []byte {
	if info.GetChannelType() != constant.ChannelTypeSGLang {
		return payload
	}
	if !gjson.GetBytes(payload, path).Exists() {
		return payload
	}
	patched, err := sjson.SetBytes(payload, path, int(createdAt))
	if err != nil {
		return payload
	}
	return patched
}
