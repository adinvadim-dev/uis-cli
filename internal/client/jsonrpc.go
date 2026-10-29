package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Timeout time.Duration
	Retries int
}

type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) String() string {
	if e == nil {
		return ""
	}
	if e.Data != nil {
		return fmt.Sprintf("%s (%v)", e.Message, e.Data)
	}
	return e.Message
}

var ErrOutcomeUnknown = errors.New("outcome_unknown: request may have committed; do not resend")

func IsRead(method string) bool {
	return strings.HasPrefix(method, "get.") || strings.HasPrefix(method, "getobj.")
}

func (c *Client) Call(ctx context.Context, method string, params map[string]any) (*Response, int, []byte, error) {
	retries := c.Retries
	if retries < 0 || !IsRead(method) {
		retries = 0
	}

	reqBody := Request{
		JSONRPC: "2.0",
		ID:      rand.IntN(1_000_000_000) + 1,
		Method:  method,
		Params:  params,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		httpClient := &http.Client{Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL, bytes.NewReader(b))
		if err != nil {
			return nil, 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = errors.New("network request failed")
			if !IsRead(method) {
				return nil, 0, nil, ErrOutcomeUnknown
			}
			if attempt == retries {
				break
			}
			sleepBackoff(attempt)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			if !IsRead(method) {
				return nil, resp.StatusCode, nil, ErrOutcomeUnknown
			}
			return nil, resp.StatusCode, nil, errors.New("incomplete response")
		}
		if !IsRead(method) && resp.StatusCode >= 500 {
			return nil, resp.StatusCode, nil, ErrOutcomeUnknown
		}

		if resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			if attempt == retries {
				return nil, resp.StatusCode, body, lastErr
			}
			sleepBackoff(attempt)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, resp.StatusCode, body, fmt.Errorf("http %d", resp.StatusCode)
		}

		var out Response
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&out); err != nil {
			if !IsRead(method) {
				return nil, resp.StatusCode, nil, ErrOutcomeUnknown
			}
			return nil, resp.StatusCode, nil, errors.New("invalid JSON response")
		}
		if out.JSONRPC != "2.0" || !matchingID(out.ID, reqBody.ID.(int)) || (len(out.Result) == 0 && out.Error == nil) {
			if !IsRead(method) {
				return nil, resp.StatusCode, nil, ErrOutcomeUnknown
			}
			return nil, resp.StatusCode, nil, errors.New("invalid RPC envelope")
		}
		return &out, resp.StatusCode, body, nil
	}

	return nil, 0, nil, lastErr
}

func sleepBackoff(attempt int) {
	base := 500 * time.Millisecond
	d := base * time.Duration(1<<attempt)
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	jitter := time.Duration(rand.IntN(250)) * time.Millisecond
	time.Sleep(d + jitter)
}

func matchingID(v interface{}, want int) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	f, e := n.Float64()
	return e == nil && f == float64(want)
}
