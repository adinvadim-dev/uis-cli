package portal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAutodialRPCJWTAndSingleDispatch(t *testing.T) {
	for _, method := range []string{"get.auto_call_campaigns", "create.auto_call_campaign_phone_numbers", "delete.auto_call_campaign_phone_numbers", "set.auto_call_campaign_status"} {
		t.Run(method, func(t *testing.T) {
			calls, probes := 0, 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v2.0" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-jwt" || r.Header.Get("X-Auth-Type") != "jwt" {
					t.Error("wrong JWT RPC contract")
				}
				var v Object
				json.NewDecoder(r.Body).Decode(&v)
				if v["method"] != method || v["id"] != "uis-autodial" {
					t.Error("wrong RPC identity")
				}
				if _, ok := v["params"].(map[string]interface{})["access_token"]; ok {
					t.Error("API key leaked into cabinet RPC")
				}
				if method == "get.auto_call_campaigns" {
					io.WriteString(w, `{"id":"uis-autodial","result":{"data":[{"id":9007199254740993}],"metadata":{}}}`)
				} else {
					io.WriteString(w, `{"id":"uis-autodial","result":{"success":true}}`)
				}
			}))
			defer s.Close()
			c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
			c.RPCURL = s.URL
			c.Verify = func(context.Context, Session) error { probes++; return nil }
			v, e := c.AutodialRPC(context.Background(), method, Object{"access_token": "DO_NOT_SEND"})
			if e != nil {
				t.Fatal(e)
			}
			if calls != 1 {
				t.Fatal("RPC replayed")
			}
			if method == "get.auto_call_campaigns" {
				if probes != 0 || ID(v["data"].([]interface{})[0].(map[string]interface{})) != "9007199254740993" {
					t.Fatal("read probe or integer fidelity")
				}
			} else if probes != 1 {
				t.Fatal("missing write preflight")
			}
		})
	}
}
func TestAutodialRPCWriteErrorsNeverReplay(t *testing.T) {
	for _, tc := range []struct {
		http int
		body string
		code int
	}{
		{401, "PRIVATE_ERROR_BODY", 3}, {403, "PRIVATE_ERROR_BODY", 4}, {429, "PRIVATE_ERROR_BODY", 8}, {503, "PRIVATE_ERROR_BODY", 9}, {201, "PRIVATE_ERROR_BODY", 9}, {408, "PRIVATE_ERROR_BODY", 9},
		{200, `<html>PRIVATE_ERROR_BODY</html>`, 9}, {200, `{"id":"other","result":{"success":true}}`, 9},
		{200, `{"id":"uis-autodial","result":{}}`, 9}, {200, `{"error":{"message":"PRIVATE_ERROR_BODY"}}`, 9},
		{200, `{"id":"uis-autodial","result":{"success":true}} {}`, 9},
	} {
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(tc.http)
			io.WriteString(w, tc.body)
		}))
		c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
		c.RPCURL = s.URL
		c.Verify = func(context.Context, Session) error { return nil }
		_, e := c.AutodialRPC(context.Background(), "set.auto_call_campaign_status", Object{"id": 1, "status": "paused"})
		var pe *Error
		if !errors.As(e, &pe) || pe.Code != tc.code || strings.Contains(e.Error(), "PRIVATE_ERROR_BODY") || calls != 1 {
			t.Fatalf("unsafe write failure: %v", e)
		}
		s.Close()
	}
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	_, e := c.AutodialRPC(context.Background(), "set.auto_call_campaign_status", nil)
	var pe *Error
	if !errors.As(e, &pe) || pe.Stage != "principal_probe_unavailable" {
		t.Fatal("write bypassed preflight")
	}
}
