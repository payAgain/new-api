package common

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const relayTimingKey = "relay_upstream_timings"

// UpstreamTiming contains no request/response content or credentials. Times in
// Events are offsets from StartedAt, while RequestOffsetMS includes preparation
// before Client.Do. Multiple connection/write events expose transport retries.
type UpstreamTiming struct {
	ChannelID       int                   `json:"channel_id"`
	StartedAt       time.Time             `json:"started_at"`
	RequestOffsetMS int64                 `json:"request_offset_ms"`
	Events          []UpstreamTimingEvent `json:"events"`
	Truncated       bool                  `json:"truncated,omitempty"`
}

type UpstreamTimingEvent struct {
	Name string `json:"name"`
	AtMS int64  `json:"at_ms"`
}

type relayTimingCapture struct {
	mu       sync.Mutex
	attempts []*UpstreamTiming
}

// TraceUpstreamRequest attaches callbacks to this individual transport call.
// The shared HTTP client is unchanged, and an existing trace is composed by
// net/http. Bounded metadata is retained for successful and failed requests.
func TraceUpstreamRequest(c *gin.Context, req *http.Request, channelID int, requestStarted time.Time) (*http.Request, func(error)) {
	value, exists := c.Get(relayTimingKey)
	var capture *relayTimingCapture
	if exists {
		capture, _ = value.(*relayTimingCapture)
	}
	if capture == nil {
		capture = &relayTimingCapture{}
		c.Set(relayTimingKey, capture)
	}
	capture.mu.Lock()
	if len(capture.attempts) >= 16 {
		capture.attempts[len(capture.attempts)-1].Truncated = true
		capture.mu.Unlock()
		return req, func(error) {}
	}
	started := time.Now()
	if requestStarted.IsZero() {
		requestStarted = started
	}
	attempt := &UpstreamTiming{ChannelID: channelID, StartedAt: started.UTC(), RequestOffsetMS: max(0, started.Sub(requestStarted).Milliseconds())}
	capture.attempts = append(capture.attempts, attempt)
	capture.mu.Unlock()
	record := func(name string) {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		if len(attempt.Events) >= 32 {
			attempt.Truncated = true
			return
		}
		attempt.Events = append(attempt.Events, UpstreamTimingEvent{Name: name, AtMS: time.Since(started).Milliseconds()})
	}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) { record("get_connection") },
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Reused {
				record("connection_reused")
			} else {
				record("connection_ready")
			}
		},
		DNSStart:          func(httptrace.DNSStartInfo) { record("dns_start") },
		DNSDone:           func(httptrace.DNSDoneInfo) { record("dns_done") },
		ConnectStart:      func(string, string) { record("connect_start") },
		ConnectDone:       func(string, string, error) { record("connect_done") },
		TLSHandshakeStart: func() { record("tls_start") },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { record("tls_done") },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err != nil {
				record("request_write_error")
			} else {
				record("request_written")
			}
		},
		GotFirstResponseByte: func() { record("first_response_byte") },
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace)), func(err error) {
		if err != nil {
			record("roundtrip_error")
		} else {
			record("response_headers_received")
		}
	}
}

func RelayUpstreamTimings(c *gin.Context) []UpstreamTiming {
	value, exists := c.Get(relayTimingKey)
	if !exists {
		return nil
	}
	capture, ok := value.(*relayTimingCapture)
	if !ok {
		return nil
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	result := make([]UpstreamTiming, 0, len(capture.attempts))
	for _, attempt := range capture.attempts {
		item := *attempt
		item.Events = append([]UpstreamTimingEvent(nil), attempt.Events...)
		result = append(result, item)
	}
	return result
}
