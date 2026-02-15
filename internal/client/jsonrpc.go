package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
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

func (c *Client) Call(ctx context.Context, method string, params map[string]any) (*Response, int, []byte, error) {
	if c.Retries < 0 {
		c.Retries = 0
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
	for attempt := 0; attempt <= c.Retries; attempt++ {
		httpClient := &http.Client{Timeout: c.Timeout}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL, bytes.NewReader(b))
		if err != nil {
			return nil, 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt == c.Retries {
				break
			}
			sleepBackoff(attempt)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, bytes.TrimSpace(body))
			if attempt == c.Retries {
				return nil, resp.StatusCode, body, lastErr
			}
			sleepBackoff(attempt)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, resp.StatusCode, body, fmt.Errorf("http %d: %s", resp.StatusCode, bytes.TrimSpace(body))
		}

		var out Response
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, resp.StatusCode, body, fmt.Errorf("invalid json response: %w", err)
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
