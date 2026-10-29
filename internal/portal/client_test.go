package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fake(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c := New(Session{GoToken: "fixture-go", AppToken: "fixture-app"}, time.Second)
	c.BaseURL = s.URL
	c.AppURL = s.URL
	c.Verify = func(context.Context, Session) error { return nil }
	return c
}
func TestCreateTwoStagesAndPartialIdentity(t *testing.T) {
	for _, mode := range []string{"success", "no_id", "comment_failure", "no_namespace"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" {
					t.Fatalf("unexpected %s", r.Method)
				}
				cookie, e := r.Cookie("access_token")
				if e != nil || cookie.Value != "fixture-go" || r.Header.Get("Authorization") != "" {
					t.Fatal("wrong support credential surface")
				}
				if e = r.ParseMultipartForm(1 << 20); e != nil {
					t.Fatal(e)
				}
				var request Object
				if e = json.Unmarshal([]byte(r.FormValue("request")), &request); e != nil {
					t.Fatal(e)
				}
				if calls == 1 {
					if r.URL.Path != "/infopin/tech-support" || request["context"] != "hello" {
						t.Fatal("wrong create contract")
					}
					switch mode {
					case "no_id":
						fmt.Fprint(w, `{"data":{}}`)
					case "no_namespace":
						fmt.Fprint(w, `{"id":123}`)
					default:
						fmt.Fprint(w, `{"id":123,"is_ticket":true}`)
					}
				} else {
					if r.URL.Path != "/infopin/tech-support-comment" || request["tech_support_id"] != float64(123) || request["message"] != "hello" || request["is_ticket"] != true {
						t.Fatal("wrong initial comment contract")
					}
					if mode == "comment_failure" {
						http.Error(w, "secret", 503)
					} else {
						fmt.Fprint(w, `{"id":321}`)
					}
				}
			})
			ticket, e := c.Create(context.Background(), Object{"context": "hello"}, nil)
			if mode == "success" {
				if e != nil || ID(ticket) != "123" || calls != 2 {
					t.Fatalf("got %v %v calls=%d", ticket, e, calls)
				}
				return
			}
			var pe *Error
			if !errors.As(e, &pe) || pe.Code != 9 {
				t.Fatalf("expected unknown, got %v", e)
			}
			if mode == "no_id" && calls != 1 {
				t.Fatal("invented ID or sent comment after missing ID")
			}
			if mode != "no_id" && pe.TicketID != "123" {
				t.Fatal("lost known ticket identity")
			}
		})
	}
}
func TestMutationRequiresPrincipalAndDoesNotReplay(t *testing.T) {
	calls := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "private secret", 403) })
	c.Verify = nil
	_, e := c.Create(context.Background(), Object{}, nil)
	var pe *Error
	if !errors.As(e, &pe) || pe.Code != 7 || calls != 0 {
		t.Fatalf("preflight %v calls=%d", e, calls)
	}
	c.Verify = func(context.Context, Session) error { return nil }
	_, e = c.Create(context.Background(), Object{}, nil)
	if !errors.As(e, &pe) || pe.Code != 4 || calls != 1 {
		t.Fatalf("permission refusal replayed: %v calls=%d", e, calls)
	}
}

func TestProbeAuthenticatedReadBeforeCreate(t *testing.T) {
	probes, tickets := 0, 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/infopin/tech-support-web-type":
			probes++
			cookie, e := r.Cookie("access_token")
			if e != nil || cookie.Value != "go" {
				t.Fatalf("auth cookie %#v %v", cookie, e)
			}
			_ = json.NewEncoder(w).Encode([]Object{{"id": 1}})
		case r.Method == "POST" && r.URL.Path == "/infopin/tech-support":
			tickets++
			_ = json.NewEncoder(w).Encode([]Object{{"id": 9, "is_ticket": true}})
		case r.Method == "POST" && r.URL.Path == "/infopin/tech-support-comment":
			_ = json.NewEncoder(w).Encode([]Object{{"id": 10}})
		default:
			http.NotFound(w, r)
		}
	})
	c.Session = Session{GoToken: "go", Login: "user@example.com"}
	c.Verify = func(ctx context.Context, session Session) error {
		probe := New(session, time.Second)
		probe.BaseURL = c.BaseURL
		probe.HTTP = c.HTTP
		return probe.ProbeAuthenticatedRead(ctx)
	}
	if _, e := c.Create(context.Background(), Object{"context": "hello"}, nil); e != nil {
		t.Fatal(e)
	}
	if probes != 1 || tickets != 1 {
		t.Fatalf("probes=%d tickets=%d", probes, tickets)
	}
	empty := New(Session{}, time.Millisecond)
	empty.BaseURL = c.BaseURL
	if e := empty.ProbeAuthenticatedRead(context.Background()); e == nil {
		t.Fatal("empty session must fail probe")
	}
}
func TestStatusReconcilesStoredNameAfterLostAck(t *testing.T) {
	puts := 0
	gets := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			puts++
			var o Object
			json.NewDecoder(r.Body).Decode(&o)
			if o["status"] != "closed" || o["is_ticket"] != true {
				t.Fatal("wrong close contract")
			}
			http.Error(w, "uncertain", 503)
			return
		}
		gets++
		status := "created"
		if gets > 1 {
			status = "close_customer"
		}
		fmt.Fprintf(w, `[{"id":123,"is_ticket":true,"status":%q}]`, status)
	})
	v, e := c.Status(context.Background(), "123", "closed")
	if e != nil || v["status"] != "close_customer" || puts != 1 || gets != 2 {
		t.Fatalf("%v %v puts=%d gets=%d", v, e, puts, gets)
	}
}
func TestSessionPromotionPreservesPreviousOnMismatch(t *testing.T) {
	dir := t.TempDir()
	old := []byte(`{"login":"previous","app_token":"fixture"}`)
	if e := os.WriteFile(filepath.Join(dir, "session.json"), old, 0600); e != nil {
		t.Fatal(e)
	}
	e := Promote(dir, Session{GoToken: "new"}, "expected", func(Session) (string, error) { return "wrong", nil })
	if e == nil {
		t.Fatal("mismatch accepted")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "session.json"))
	if string(b) != string(old) {
		t.Fatal("old session lost")
	}
	if e = Promote(dir, Session{GoToken: "new"}, "expected", func(Session) (string, error) { return "expected", nil }); e != nil {
		t.Fatal(e)
	}
	st, _ := os.Stat(filepath.Join(dir, "session.json"))
	if st.Mode().Perm() != 0600 {
		t.Fatal("insecure session permissions")
	}
}
func TestCallIdentityAndCredentialSurface(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analytics/call/call_details_data/" || r.URL.Query().Get("call_id") != "123" || r.Header.Get("Authorization") != "Bearer fixture-app" || r.Header.Get("Cookie") != "" {
			t.Fatal("wrong private call wire contract")
		}
		fmt.Fprint(w, `{"data":{"header":{"id":999},"grid":[]}}`)
	})
	if _, e := c.Call(context.Background(), "123"); e == nil {
		t.Fatal("mismatched call identity accepted")
	}
}

func TestReopenOldTicketFailsBeforeWrite(t *testing.T) {
	writes := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		fmt.Fprint(w, `[{"id":123,"is_ticket":true,"status":"close_customer","date_close":"2000-01-01"}]`)
	})
	_, e := c.Status(context.Background(), "123", "created")
	var pe *Error
	if !errors.As(e, &pe) || pe.Code != 2 || writes != 0 {
		t.Fatalf("old reopen dispatched: %v writes=%d", e, writes)
	}
}
