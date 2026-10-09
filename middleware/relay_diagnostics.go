package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// RelayDiagnostics observes HTTP bodies without consuming them in advance.
// Authentication and management APIs keep their separate audit rules. Raw relay
// payloads are persisted only for authenticated relay failures in root_info.
func RelayDiagnostics() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !constant.ErrorLogEnabled || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}
		diagnostics := common.EnableRelayDiagnostics(c)
		if diagnostics == nil {
			c.Next()
			return
		}
		writer := &diagnosticResponseWriter{ResponseWriter: c.Writer, diagnostics: diagnostics}
		c.Writer = writer
		c.Next()
		if c.GetString(RouteTagKey) != "relay" || c.GetInt("id") <= 0 || c.Writer.Status() < 400 || service.HasRecordedRelayFailure(c) {
			return
		}
		message := writer.body()
		if message == "" {
			message = http.StatusText(c.Writer.Status())
		}
		err := types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, c.Writer.Status())
		service.RecordRequestPolicyTermination(c, err)
		service.RecordRelayFailure(c, err, nil)
	}
}

type diagnosticResponseWriter struct {
	gin.ResponseWriter
	diagnostics *common.RelayDiagnostics
}

func (w *diagnosticResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.diagnostics.CaptureClientResponse(data[:n])
	return n, err
}

func (w *diagnosticResponseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *diagnosticResponseWriter) body() string {
	return w.diagnostics.Snapshot("", "", "", w.Status()).ClientResponse.Body.Data
}
