package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadRetryAndMutationSendOnce(t *testing.T) {
	for _, method := range []string{"get.calls_report", "create.communication_comment", "set.tag_communications", "unknown.method"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					http.Error(w, "DO_NOT_LOG_PROVIDER_BODY", 503)
					return
				}
				var req Request
				if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
					t.Fatal(e)
				}
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"result":{"data":[]}}`, req.ID)
			}))
			defer s.Close()
			c := Client{BaseURL: s.URL, Timeout: time.Second, Retries: 1}
			resp, _, _, e := c.Call(context.Background(), method, map[string]interface{}{})
			if method == "get.calls_report" {
				if e != nil || resp == nil || calls != 2 {
					t.Fatalf("read retry: calls=%d err=%v", calls, e)
				}
			} else if calls != 1 || !errors.Is(e, ErrOutcomeUnknown) {
				t.Fatalf("mutation must send once: calls=%d err=%v", calls, e)
			}
		})
	}
}
func TestMalformedMutationResponseIsUnknown(t *testing.T) {
	for _, body := range []string{`<html>login</html>`, `{}`, `{"jsonrpc":"2.0","id":0,"result":{}}`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer s.Close()
			c := Client{BaseURL: s.URL}
			_, _, _, e := c.Call(context.Background(), "create.anything", nil)
			if !errors.Is(e, ErrOutcomeUnknown) {
				t.Fatalf("expected unknown, got %v", e)
			}
		})
	}
}
