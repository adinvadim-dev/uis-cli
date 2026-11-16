package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

const staleSession = `{"login":"fixture@example.invalid","cabinet_token":"stale-cabinet","go_token":"stale-go","app_token":"stale-app"}`

// refreshFixture routes real provider hostnames to one httptest server. Login
// skips captcha (the legacy redirect lands on the app callback directly), so a
// refresh is fast and needs no paid solver.
type refreshFixture struct {
	t                      *testing.T
	logins, reports, posts int
	failLogin, rejectAll   bool
	sessionPath            string
	reportBody             string                      // report-constructor success body
	routes                 map[string]http.HandlerFunc // extra host+path handlers
}

func newRefreshFixture(t *testing.T) *refreshFixture {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("UIS_TOKEN", "")
	t.Setenv(noAutoRefreshEnv, "")
	a := profile.Account{ExpectedLogin: "fixture@example.invalid"}
	a.CredentialProcess.Command = []string{"sh", "-c", `printf '%s' '{"schemaVersion":"uis.credential-process/v1","fields":{"portal.login":"fixture@example.invalid","portal.password":"fixture-password"}}'`}
	if err := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": a}}); err != nil {
		t.Fatal(err)
	}
	f := &refreshFixture{t: t, sessionPath: filepath.Join(profile.State("fixture"), "session.json"), routes: map[string]http.HandlerFunc{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			f.posts++
		}
		if h := f.routes[r.Host+r.URL.Path]; h != nil {
			h(w, r)
			return
		}
		switch r.Host + r.URL.Path {
		case "www.uiscom.ru/api/":
			f.logins++
			if f.failLogin {
				fmt.Fprint(w, `{"success":false}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"redirect_url":"https://app.uiscom.ru/sso/callback"}`)
		case "app.uiscom.ru/sso/callback":
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "fresh-app", Path: "/"})
			fmt.Fprint(w, "ok")
		case "uis-prod-msk-comagic-api.uiscom.ru/billing/service":
			http.Redirect(w, r, "https://uis-prod-msk-comagic-api.uiscom.ru/callback", 302)
		case "uis-prod-msk-comagic-api.uiscom.ru/callback":
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "fresh-go", Path: "/"})
			fmt.Fprint(w, "ok")
		case "go.uiscom.ru/":
			http.SetCookie(w, &http.Cookie{Name: "auth", Value: url.QueryEscape(`{"jwt":"fresh-cabinet"}`), Domain: ".uiscom.ru", Path: "/"})
			fmt.Fprint(w, "ok")
		case "uis-prod-msk-comagic-api.uiscom.ru/infopin/tech-support-web-type":
			if c, e := r.Cookie("access_token"); e != nil || c.Value != "fresh-go" {
				http.Error(w, "expired", 401)
				return
			}
			fmt.Fprint(w, `[{"id":1,"name":"topic"}]`)
		case "uis-prod-msk-report-constructor-api.uiscom.ru/builder/report":
			f.reports++
			if f.rejectAll || r.Header.Get("Authorization") != "Bearer fresh-cabinet" {
				http.Error(w, "expired", 401)
				return
			}
			body := f.reportBody
			if body == "" {
				body = `{"data":[{"communication_id":123}],"metadata":{"total_items":1}}`
			}
			fmt.Fprint(w, body)
		default:
			t.Errorf("unexpected wire request %s %s", r.Method, r.Host+r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	t.Cleanup(server.Close)
	dest, _ := url.Parse(server.URL)
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		c := r.Clone(r.Context())
		u := *r.URL
		c.URL = &u
		c.URL.Scheme = dest.Scheme
		c.URL.Host = dest.Host
		c.Host = r.URL.Host
		return old.RoundTrip(c)
	})
	f.writeSession(staleSession)
	return f
}

func (f *refreshFixture) writeSession(body string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.sessionPath), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(f.sessionPath, []byte(body), 0600); err != nil {
		f.t.Fatal(err)
	}
}

type refreshResult struct {
	code    int
	out     string
	details map[string]interface{}
}

func (f *refreshFixture) run(args ...string) refreshResult {
	f.t.Helper()
	var r refreshResult
	r.out = capture(f.t, func() { r.code = Run(append([]string{"--json"}, args...)) })
	for _, secret := range []string{"fixture-password", "fresh-app", "fresh-go", "fresh-cabinet", "stale-cabinet"} {
		if strings.Contains(r.out, secret) {
			f.t.Fatalf("secret in output: %s", r.out)
		}
	}
	if r.code != 0 {
		var envelope struct {
			Error struct {
				Details map[string]interface{} `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(r.out), &envelope); err != nil {
			f.t.Fatalf("bad error envelope: %s", r.out)
		}
		r.details = envelope.Error.Details
	}
	return r
}

var privateSearch = []string{"calls", "search", "--source", "private", "--from", "2026-11-16 00:00:00", "--till", "2026-11-16 12:00:00", "--limit", "1"}

// expectHeal proves the harness can observe a refresh before a negative case
// asserts its absence, then restores the expired session.
func (f *refreshFixture) expectHeal() {
	f.t.Helper()
	logins := f.logins
	if r := f.run(privateSearch...); r.code != 0 || f.logins != logins+1 {
		f.t.Fatalf("control read did not self-heal: exit=%d logins=%d %s", r.code, f.logins-logins, r.out)
	}
	f.writeSession(staleSession)
}

func TestPrivateReadAutoRefresh(t *testing.T) {
	t.Run("expired_session_refreshes_once_and_retries", func(t *testing.T) {
		f := newRefreshFixture(t)
		stderr, err := os.CreateTemp(t.TempDir(), "stderr")
		if err != nil {
			t.Fatal(err)
		}
		oldStderr := os.Stderr
		os.Stderr = stderr
		r := f.run(append([]string{"--verbose"}, privateSearch...)...)
		os.Stderr = oldStderr
		if r.code != 0 || !strings.Contains(r.out, "private.report_constructor") {
			t.Fatalf("exit=%d %s", r.code, r.out)
		}
		if f.logins != 1 || f.reports != 2 {
			t.Fatalf("logins=%d reports=%d, want one refresh and one retry", f.logins, f.reports)
		}
		s, err := portal.LoadSession(filepath.Dir(f.sessionPath))
		if err != nil || s.CabinetToken != "fresh-cabinet" || s.GoToken != "fresh-go" || s.Login != "fixture@example.invalid" || s.UpdatedAt.IsZero() {
			t.Fatal("refreshed session was not promoted")
		}
		notice, _ := os.ReadFile(stderr.Name())
		if lines := strings.Split(strings.TrimSpace(string(notice)), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "refreshing it once") {
			t.Fatalf("want one verbose notice line, got %q", notice)
		}
	})

	t.Run("missing_session_refreshes_explicit_private_read", func(t *testing.T) {
		f := newRefreshFixture(t)
		if err := os.Remove(f.sessionPath); err != nil {
			t.Fatal(err)
		}
		if r := f.run(privateSearch...); r.code != 0 || f.logins != 1 || f.reports != 1 {
			t.Fatalf("exit=%d logins=%d reports=%d %s", r.code, f.logins, f.reports, r.out)
		}
	})

	t.Run("refresh_failure_returns_original_error", func(t *testing.T) {
		f := newRefreshFixture(t)
		f.failLogin = true
		r := f.run(privateSearch...)
		if r.code != 3 || r.details["stage"] != "cabinet_http" || r.details["recovery"] != privateAuthRecovery {
			t.Fatalf("original auth error lost: %s", r.out)
		}
		attempt, _ := r.details["auto_refresh"].(map[string]interface{})
		if attempt["attempted"] != true || attempt["outcome"] != "refresh_failed" || attempt["error_code"] != "authentication_required" {
			t.Fatalf("auto refresh outcome missing: %s", r.out)
		}
		if f.logins != 1 || f.reports != 1 {
			t.Fatalf("logins=%d reports=%d, want one refresh and no retry", f.logins, f.reports)
		}
		if b, _ := os.ReadFile(f.sessionPath); string(b) != staleSession {
			t.Fatal("failed refresh changed session")
		}
	})

	t.Run("retry_still_rejected_returns_original_error", func(t *testing.T) {
		f := newRefreshFixture(t)
		f.rejectAll = true
		r := f.run(privateSearch...)
		attempt, _ := r.details["auto_refresh"].(map[string]interface{})
		if r.code != 3 || r.details["recovery"] != privateAuthRecovery || attempt["outcome"] != "retry_failed" || attempt["error_code"] != "authentication_required" {
			t.Fatalf("exit=%d %s", r.code, r.out)
		}
		if f.logins != 1 || f.reports != 2 {
			t.Fatalf("logins=%d reports=%d, want exactly one retry", f.logins, f.reports)
		}
	})

	t.Run("write_never_refreshes", func(t *testing.T) {
		f := newRefreshFixture(t)
		f.expectHeal()
		logins, posts := f.logins, f.posts
		r := f.run("--force", "support", "ticket", "reply", "123", "--body", "test")
		if r.code != 3 || r.details["auto_refresh"] != nil {
			t.Fatalf("write must fail closed without refresh: %s", r.out)
		}
		if f.logins != logins || f.posts != posts {
			t.Fatal("write path refreshed or dispatched")
		}
	})

	t.Run("opt_out_env_disables_refresh", func(t *testing.T) {
		f := newRefreshFixture(t)
		f.expectHeal()
		t.Setenv(noAutoRefreshEnv, "1")
		logins := f.logins
		r := f.run(privateSearch...)
		if r.code != 3 || r.details["recovery"] != privateAuthRecovery || r.details["auto_refresh"] != nil || f.logins != logins {
			t.Fatalf("opt-out ignored: logins=%d %s", f.logins-logins, r.out)
		}
	})

	t.Run("principal_mismatch_fails_closed", func(t *testing.T) {
		f := newRefreshFixture(t)
		f.expectHeal()
		mismatched := strings.Replace(staleSession, "fixture@example.invalid", "other@example.invalid", 1)
		f.writeSession(mismatched)
		logins, reports := f.logins, f.reports
		r := f.run(privateSearch...)
		if r.code != 3 || r.details["auto_refresh"] != nil || f.logins != logins || f.reports != reports {
			t.Fatalf("mismatch refreshed or reached network: %s", r.out)
		}
		if b, _ := os.ReadFile(f.sessionPath); string(b) != mismatched {
			t.Fatal("mismatch changed session")
		}
	})
}
