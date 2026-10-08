package workbuddy

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	native "github.com/QuantumNous/new-api/pkg/workbuddy"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Adaptor converts all supported client formats into WorkBuddy's chat protocol;
// response conversion and billing remain in New API's existing handlers.
type Adaptor struct{ openai.Adaptor }

func (a *Adaptor) GetChannelName() string { return "workbuddy" }
func (a *Adaptor) GetModelList() []string { return nil }
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, r dto.OpenAIResponsesRequest) (any, error) {
	if r.PreviousResponseID != "" || len(r.Conversation) > 0 || len(r.ContextManagement) > 0 {
		return nil, errors.New("WorkBuddy requires complete input history; previous_response_id is not supported")
	}
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, &r)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	if info.RelayMode != relayconstant.RelayModeChatCompletions && info.RelayMode != relayconstant.RelayModeResponses {
		return nil, errors.New("WorkBuddy supports chat completions and Responses only")
	}
	channel, err := model.GetChannelById(info.ChannelId, true)
	if err != nil {
		return nil, errors.New("WorkBuddy channel does not exist")
	}
	pool, err := native.ForChannel(channel)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return nil, errors.New("WorkBuddy request exceeds size limit")
	}
	resp, err := pool.Chat(c.Request.Context(), info.UpstreamModelName, data)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 200 && !info.IsStream {
		return native.AggregateResponse(resp)
	}
	return resp, nil
}
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	info.FinalRequestRelayFormat = types.RelayFormatOpenAI
	if info.RelayMode == relayconstant.RelayModeResponses {
		if info.IsStream {
			return openai.OaiChatToResponsesStreamHandler(c, info, resp)
		}
		return openai.OaiChatToResponsesHandler(c, info, resp)
	}
	return a.Adaptor.DoResponse(c, resp, info)
}
