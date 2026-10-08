package workbuddy

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// AggregateResponse preserves upstream usage verbatim and rejects truncated or
// business-error SSE rather than manufacturing a successful JSON completion.
func AggregateResponse(resp *http.Response) (*http.Response, error) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	result := map[string]any{"object": "chat.completion"}
	choices := make(map[int]map[string]any)
	tools := make(map[int]map[int]map[string]any)
	var data strings.Builder
	total := 0
	done := false
	consume := func() error {
		event := data.String()
		data.Reset()
		if event == "" {
			return nil
		}
		if event == "[DONE]" {
			done = true
			return nil
		}
		var chunk map[string]any
		if common.UnmarshalJsonStr(event, &chunk) != nil || chunk["error"] != nil {
			return errors.New("WorkBuddy upstream returned invalid or error SSE")
		}
		for _, k := range []string{"id", "model", "created", "system_fingerprint"} {
			if v, ok := chunk[k]; ok {
				result[k] = v
			}
		}
		if usage, ok := chunk["usage"].(map[string]any); ok {
			result["usage"] = usage
		}
		entries, _ := chunk["choices"].([]any)
		for _, entry := range entries {
			c, ok := entry.(map[string]any)
			if !ok {
				return errors.New("invalid WorkBuddy choice")
			}
			number, ok := c["index"].(float64)
			if !ok || number < 0 || number > 127 || number != float64(int(number)) {
				return errors.New("invalid WorkBuddy choice index")
			}
			index := int(number)
			choice := choices[index]
			if choice == nil {
				choice = map[string]any{"index": index, "message": map[string]any{"role": "assistant", "content": ""}}
				choices[index] = choice
			}
			if reason, ok := c["finish_reason"].(string); ok && reason != "" {
				choice["finish_reason"] = reason
			}
			message := choice["message"].(map[string]any)
			delta, _ := c["delta"].(map[string]any)
			for _, k := range []string{"content", "reasoning_content", "reasoning"} {
				if text, ok := delta[k].(string); ok {
					old, _ := message[k].(string)
					message[k] = old + text
				}
			}
			if annotations, ok := delta["annotations"].([]any); ok {
				previous, _ := message["annotations"].([]any)
				message["annotations"] = append(previous, annotations...)
			}
			calls, _ := delta["tool_calls"].([]any)
			for _, call := range calls {
				t, ok := call.(map[string]any)
				if !ok {
					return errors.New("invalid WorkBuddy tool call")
				}
				n, ok := t["index"].(float64)
				if !ok || n < 0 || n > 127 || n != float64(int(n)) {
					return errors.New("invalid WorkBuddy tool index")
				}
				if tools[index] == nil {
					tools[index] = make(map[int]map[string]any)
				}
				tool := tools[index][int(n)]
				if tool == nil {
					tool = map[string]any{"type": "function", "function": map[string]any{"name": "", "arguments": ""}}
					tools[index][int(n)] = tool
				}
				if value, ok := t["id"].(string); ok {
					tool["id"] = value
				}
				if value, ok := t["type"].(string); ok {
					tool["type"] = value
				}
				f, _ := t["function"].(map[string]any)
				target := tool["function"].(map[string]any)
				for _, k := range []string{"name", "arguments"} {
					if fragment, ok := f[k].(string); ok {
						target[k] = target[k].(string) + fragment
					}
				}
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		total += len(line)
		if total > 64<<20 {
			return nil, errors.New("WorkBuddy response exceeds size limit")
		}
		if line == "" {
			if err := consume(); err != nil {
				return nil, err
			}
			if done {
				break
			}
		} else if fragment, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(fragment, " "))
		}
	}
	if scanner.Err() != nil || data.Len() > 0 || len(choices) == 0 {
		return nil, errors.New("WorkBuddy upstream stream was interrupted")
	}
	indexes := make([]int, 0, len(choices))
	for i := range choices {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	output := make([]any, 0, len(indexes))
	for _, index := range indexes {
		choice := choices[index]
		if choice["finish_reason"] == nil {
			return nil, errors.New("WorkBuddy upstream stream has no finish reason")
		}
		if calls := tools[index]; len(calls) > 0 {
			indices := make([]int, 0, len(calls))
			for i := range calls {
				indices = append(indices, i)
			}
			sort.Ints(indices)
			items := make([]any, 0, len(indices))
			for _, i := range indices {
				items = append(items, calls[i])
			}
			choice["message"].(map[string]any)["tool_calls"] = items
		}
		output = append(output, choice)
	}
	result["choices"] = output
	raw, err := common.Marshal(result)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	resp.ContentLength = int64(len(raw))
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}
