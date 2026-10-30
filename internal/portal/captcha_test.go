package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCaptchaOutcomesNeverDuplicatePaidTask(t *testing.T) {
	for _, tc := range []struct {
		name, create, result string
		status               int
		want                 string
		ambiguous            bool
	}{
		{"ready", `{"errorId":0,"taskId":7}`, `{"errorId":0,"status":"ready","solution":{"token":"solution"}}`, 200, "", false},
		{"provider", `{"errorId":1,"errorDescription":"secret"}`, "", 200, "createTask_provider", false},
		{"uncertain", `secret`, "", 200, "createTask_schema", true},
		{"http", `secret`, "", 503, "createTask_http", true},
		{"missing-id", `{"errorId":0}`, "", 200, "createTask_identity", true},
		{"missing-solution", `{"errorId":0,"taskId":7}`, `{"errorId":0,"status":"ready"}`, 200, "result_schema", false},
		{"poll-provider", `{"errorId":0,"taskId":7}`, `{"errorId":1,"errorDescription":"secret"}`, 200, "getTaskResult_provider", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creates, polls := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/createTask" {
					creates++
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.create)
				} else {
					polls++
					if polls == 1 {
						fmt.Fprint(w, `{"errorId":0,"status":"processing"}`)
					} else {
						fmt.Fprint(w, tc.result)
					}
				}
			}))
			defer srv.Close()
			dest, _ := url.Parse(srv.URL)
			base := http.DefaultTransport
			solver := NewCaptcha(transportFunc(func(r *http.Request) (*http.Response, error) {
				r.URL.Scheme = dest.Scheme
				r.URL.Host = dest.Host
				return base.RoundTrip(r)
			}))
			solver.PollInterval = time.Millisecond
			token, err := solver.Solve(context.Background(), "secret", "https://sso.uiscom.ru/login", "site")
			if tc.want == "" {
				if err != nil || token != "solution" {
					t.Fatalf("token=%s err=%v", token, err)
				}
			} else {
				var ce *CaptchaError
				if !errors.As(err, &ce) || ce.Stage != tc.want || ce.Ambiguous != tc.ambiguous || strings.Contains(err.Error(), "secret") {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if creates != 1 {
				t.Fatalf("paid creates=%d", creates)
			}
		})
	}
}
func TestCaptchaCancellationDoesNotCreateAnotherTask(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	solver := NewCaptcha(transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, errors.New("secret transport")
	}))
	_, err := solver.Solve(ctx, "secret", "https://sso.uiscom.ru/login", "site")
	var ce *CaptchaError
	if calls != 1 || !errors.As(err, &ce) || !ce.Ambiguous || strings.Contains(err.Error(), "secret") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
