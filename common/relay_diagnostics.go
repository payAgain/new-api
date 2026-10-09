package common

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const relayDiagnosticsKey = "relay_error_diagnostics"

// DiagnosticBody preserves captured bytes without masking. Binary payloads use
// base64; observed byte counts and truncation describe the capture, not usage.
type DiagnosticBody struct {
	Data          string `json:"data"`
	Encoding      string `json:"encoding"`
	Bytes         int64  `json:"bytes"`
	CapturedBytes int    `json:"captured_bytes"`
	Truncated     bool   `json:"truncated"`
	ReadError     string `json:"read_error,omitempty"`
}

type DiagnosticExchange struct {
	Method           string         `json:"method,omitempty"`
	URL              string         `json:"url,omitempty"`
	Headers          http.Header    `json:"headers,omitempty"`
	HeadersTruncated bool           `json:"headers_truncated,omitempty"`
	Status           int            `json:"status,omitempty"`
	Protocol         string         `json:"protocol,omitempty"`
	Body             DiagnosticBody `json:"body"`
}

type DiagnosticAttempt struct {
	ID                int                `json:"id"`
	ChannelID         int                `json:"channel_id"`
	ChannelName       string             `json:"channel_name,omitempty"`
	Model             string             `json:"model,omitempty"`
	RequestFormat     string             `json:"request_format,omitempty"`
	UpstreamRequestID string             `json:"upstream_request_id,omitempty"`
	ElapsedMS         int64              `json:"elapsed_ms"`
	Error             string             `json:"error,omitempty"`
	Request           DiagnosticExchange `json:"request"`
	Response          DiagnosticExchange `json:"response"`
}

type RelayDiagnosticSnapshot struct {
	Version         int                 `json:"version"`
	RequestID       string              `json:"request_id,omitempty"`
	Stage           string              `json:"stage"`
	Error           string              `json:"error"`
	Status          int                 `json:"status"`
	ErrorCode       string              `json:"error_code,omitempty"`
	ErrorType       string              `json:"error_type,omitempty"`
	ElapsedMS       int64               `json:"elapsed_ms"`
	ClientRequest   DiagnosticExchange  `json:"client_request"`
	ClientResponse  DiagnosticExchange  `json:"client_response"`
	Attempts        []DiagnosticAttempt `json:"attempts"`
	OmittedAttempts int                 `json:"omitted_attempts,omitempty"`
}

type diagnosticCapture struct {
	data      []byte
	bytes     int64
	readError string
}

type diagnosticExchangeCapture struct {
	DiagnosticExchange
	capture diagnosticCapture
}

type diagnosticAttemptCapture struct {
	DiagnosticAttempt
	started  time.Time
	request  diagnosticExchangeCapture
	response diagnosticExchangeCapture
}

// RelayDiagnostics buffers bounded observations in memory. Only failed requests
// persist a Snapshot in root_info; successful request bodies are discarded.
// All mutation and snapshots share a lock because net/http can read a request
// body concurrently with its response and transport errors.
type RelayDiagnostics struct {
	mu             sync.Mutex
	requestID      string
	started        time.Time
	stage          string
	bodyLimit      int
	remaining      int
	attemptLimit   int
	omitted        int
	client         diagnosticExchangeCapture
	clientResponse diagnosticExchangeCapture
	attempts       []*diagnosticAttemptCapture
	current        *diagnosticAttemptCapture
}

func EnableRelayDiagnostics(c *gin.Context) *RelayDiagnostics {
	if existing := GetRelayDiagnostics(c); existing != nil {
		return existing
	}
	if !GetEnvOrDefaultBool("ERROR_DIAGNOSTICS_ENABLED", true) {
		return nil
	}
	d := &RelayDiagnostics{
		requestID: c.GetString(RequestIdKey),
		started:   time.Now(), stage: "routing",
		bodyLimit:    min(max(GetEnvOrDefault("ERROR_DIAGNOSTICS_MAX_BODY_KB", 1024), 1), 16384) * 1024,
		remaining:    min(max(GetEnvOrDefault("ERROR_DIAGNOSTICS_MAX_TOTAL_KB", 4096), 1), 65536) * 1024,
		attemptLimit: min(max(GetEnvOrDefault("ERROR_DIAGNOSTICS_MAX_ATTEMPTS", 16), 1), 64),
	}
	d.client.Method, d.client.URL = c.Request.Method, c.Request.URL.String()
	d.client.Headers, d.client.HeadersTruncated = d.captureHeaders(c.Request.Header)
	c.Set(relayDiagnosticsKey, d)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), relayDiagnosticsKey, d))
	if c.Request.Body != nil {
		c.Request.Body = &diagnosticReadCloser{ReadCloser: c.Request.Body, owner: d, capture: &d.client.capture}
	}
	return d
}

func GetRelayDiagnostics(ctx context.Context) *RelayDiagnostics {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(relayDiagnosticsKey).(*RelayDiagnostics)
	return d
}

func (d *RelayDiagnostics) SetStage(stage string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stage = stage
}

func (d *RelayDiagnostics) BeginAttempt(channelID int, name, model, format string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.current = nil
	if len(d.attempts) >= d.attemptLimit {
		d.omitted++
		return
	}
	d.current = &diagnosticAttemptCapture{started: time.Now(), DiagnosticAttempt: DiagnosticAttempt{ID: len(d.attempts) + 1, ChannelID: channelID, ChannelName: name, Model: model, RequestFormat: format}}
	d.attempts = append(d.attempts, d.current)
}

func (d *RelayDiagnostics) CaptureRequest(req *http.Request) {
	if d == nil || req == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stage = "upstream_request"
	if d.current == nil {
		return
	}
	a := d.current
	if a.request.URL != "" {
		a.ElapsedMS = time.Since(a.started).Milliseconds()
		if len(d.attempts) >= d.attemptLimit {
			d.omitted++
			d.current = nil
			return
		}
		a = &diagnosticAttemptCapture{started: time.Now(), DiagnosticAttempt: DiagnosticAttempt{ID: len(d.attempts) + 1, ChannelID: a.ChannelID, ChannelName: a.ChannelName, Model: a.Model, RequestFormat: a.RequestFormat}}
		d.current = a
		d.attempts = append(d.attempts, a)
	}
	a.request.Method, a.request.URL = req.Method, req.URL.String()
	a.request.Headers, a.request.HeadersTruncated = d.captureHeaders(req.Header)
	if req.Body != nil {
		req.Body = &diagnosticReadCloser{ReadCloser: req.Body, owner: d, capture: &a.request.capture}
	}
}

// SetAttemptModel runs after channel model mapping and protocol conversion.
func (d *RelayDiagnostics) SetAttemptModel(model, format string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current != nil {
		d.current.Model, d.current.RequestFormat = model, format
	}
}

// CaptureResponse records the original HTTP status before any channel mapping.
// The wrapped reader preserves streaming and does not pre-read or drain a body.
func (d *RelayDiagnostics) CaptureResponse(resp *http.Response, transportErr error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return
	}
	a := d.current
	if transportErr != nil {
		a.Error = diagnosticMessage(transportErr.Error())
	}
	if resp == nil {
		return
	}
	d.stage = "upstream_response"
	a.response.Status, a.response.Protocol = resp.StatusCode, resp.Proto
	a.response.Headers, a.response.HeadersTruncated = d.captureHeaders(resp.Header)
	a.UpstreamRequestID = UpstreamRequestID(resp.Header)
	if resp.Body != nil {
		resp.Body = &diagnosticReadCloser{ReadCloser: resp.Body, owner: d, capture: &a.response.capture}
	}
}

func UpstreamRequestID(headers http.Header) string {
	for _, key := range []string{RequestIdKey, "X-Request-Id", "Request-Id", "X-Amzn-Requestid", "X-Goog-Request-Id", "Cf-Ray"} {
		if value := headers.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func (d *RelayDiagnostics) CaptureClientResponse(data []byte) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.captureBytes(&d.clientResponse.capture, data, nil)
}

// CaptureWebSocketFrame keeps the observed request/response events as newline
// separated frames. It uses the same body and request-wide budget as HTTP.
func (d *RelayDiagnostics) CaptureWebSocketFrame(outbound bool, data []byte, err error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return
	}
	capture := &d.current.response.capture
	if outbound {
		capture = &d.current.request.capture
	}
	d.captureBytes(capture, data, err)
	if len(data) > 0 {
		d.captureBytes(capture, []byte("\n"), nil)
	}
}

func (d *RelayDiagnostics) Snapshot(message, code, errorType string, status int) RelayDiagnosticSnapshot {
	message = diagnosticMessage(message)
	d.mu.Lock()
	defer d.mu.Unlock()
	result := RelayDiagnosticSnapshot{Version: 1, RequestID: d.requestID, Stage: d.stage, Error: message, Status: status, ErrorCode: code, ErrorType: errorType, ElapsedMS: time.Since(d.started).Milliseconds(), ClientRequest: d.exchangeSnapshot(d.client), ClientResponse: d.exchangeSnapshot(d.clientResponse), OmittedAttempts: d.omitted, Attempts: make([]DiagnosticAttempt, 0, len(d.attempts))}
	result.ClientResponse.Status = status
	for _, a := range d.attempts {
		item := a.DiagnosticAttempt
		if a == d.current && item.Error == "" {
			item.Error = message
		}
		if a.ElapsedMS == 0 {
			item.ElapsedMS = time.Since(a.started).Milliseconds()
		}
		item.Request, item.Response = d.exchangeSnapshot(a.request), d.exchangeSnapshot(a.response)
		result.Attempts = append(result.Attempts, item)
	}
	return result
}

func (d *RelayDiagnostics) FinishAttempt(message string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current != nil {
		if d.current.Error == "" {
			d.current.Error = diagnosticMessage(message)
		}
		d.current.ElapsedMS = time.Since(d.current.started).Milliseconds()
	}
}

func (d *RelayDiagnostics) exchangeSnapshot(e diagnosticExchangeCapture) DiagnosticExchange {
	result := e.DiagnosticExchange
	result.Body = DiagnosticBody{Bytes: e.capture.bytes, CapturedBytes: len(e.capture.data), Truncated: e.capture.bytes > int64(len(e.capture.data)), ReadError: e.capture.readError, Encoding: "utf-8"}
	if utf8.Valid(e.capture.data) {
		result.Body.Data = string(e.capture.data)
	} else {
		result.Body.Encoding, result.Body.Data = "base64", base64.StdEncoding.EncodeToString(e.capture.data)
	}
	return result
}

func (d *RelayDiagnostics) captureHeaders(headers http.Header) (http.Header, bool) {
	result := make(http.Header)
	budget := min(d.remaining, 64*1024)
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	truncated := false
	for _, key := range keys {
		for _, value := range headers[key] {
			if len(key)+len(value) > budget {
				truncated = true
				continue
			}
			result[key] = append(result[key], value)
			budget -= len(key) + len(value)
			d.remaining -= len(key) + len(value)
		}
	}
	return result, truncated
}

func (d *RelayDiagnostics) captureBytes(capture *diagnosticCapture, data []byte, err error) {
	capture.bytes += int64(len(data))
	count := min(len(data), d.bodyLimit-len(capture.data), d.remaining)
	capture.data = append(capture.data, data[:count]...)
	d.remaining -= count
	if err != nil && err != io.EOF {
		capture.readError = diagnosticMessage(err.Error())
	}
}

func diagnosticMessage(message string) string {
	const limit = 16 * 1024
	if len(message) <= limit {
		return message
	}
	return message[:limit] + "\n[diagnostic error text truncated]"
}

type diagnosticReadCloser struct {
	io.ReadCloser
	owner   *RelayDiagnostics
	capture *diagnosticCapture
}

func (r *diagnosticReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.owner.mu.Lock()
	r.owner.captureBytes(r.capture, p[:n], err)
	r.owner.mu.Unlock()
	return n, err
}
