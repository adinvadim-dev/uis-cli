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

// Route real provider hostnames through httptest without changing URL/cookie
// semantics in the client. No live request or paid captcha can escape this test.
func TestPrivateLoginRefreshAndReads(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", dir)
	a := profile.Account{ExpectedLogin: "FIXTURE@example.invalid"}
	a.CredentialProcess.Command = []string{"sh", "-c", `printf '%s' '{"schemaVersion":"uis.credential-process/v1","fields":{"portal.login":"fixture@example.invalid","portal.password":"fixture-password","captcha.2captcha_api_key":"fixture-captcha-key"}}'`}
	if err := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": a}}); err != nil {
		t.Fatal(err)
	}
	failProbe := false
	badRedirect := false
	creates := 0
	posts := 0
	probes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
		}
		switch r.Host + r.URL.Path {
		case "www.uiscom.ru/api/":
			r.ParseForm()
			if r.Form.Get("params[password]") != "fixture-password" || r.Form.Get("method") != "login" {
				t.Error("legacy form")
			}
			target := "https://sso.uiscom.ru/login?private=do-not-send"
			if badRedirect {
				target = "https://evil.example/login"
			}
			fmt.Fprintf(w, `{"success":true,"redirect_url":%q}`, target)
		case "sso.uiscom.ru/login":
			if r.Method == "GET" {
				fmt.Fprint(w, `<script>const APP_DATA = {"form_data":{"csrf":"fixture-csrf","not_trusted_device":"false"},"captcha":{"type":"yandex","site_key":"public-site"}};</script>`)
				return
			}
			r.ParseForm()
			if r.Form.Get("csrf") != "fixture-csrf" || r.Form.Get("smart-token") != "fixture-solution" || r.Form.Get("password") != "fixture-password" {
				t.Error("SSO form")
			}
			http.SetCookie(w, &http.Cookie{Name: "sso", Value: "fixture-sso", Path: "/"})
			http.Redirect(w, r, "https://app.uiscom.ru/sso/callback", 302)
		case "go.uiscom.ru/":
			http.SetCookie(w, &http.Cookie{Name: "auth", Value: url.QueryEscape(`{"jwt":"fixture-cabinet"}`), Domain: ".uiscom.ru", Path: "/"})
			fmt.Fprint(w, "ok")
		case "app.uiscom.ru/sso/callback":
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "fixture-app", Path: "/"})
			fmt.Fprint(w, "ok")
		case "uis-prod-msk-comagic-api.uiscom.ru/billing/service":
			http.Redirect(w, r, "https://sso.uiscom.ru/oauth2/authorize?prompt=none", 302)
		case "sso.uiscom.ru/oauth2/authorize":
			if c, e := r.Cookie("sso"); e != nil || c.Value != "fixture-sso" {
				t.Error("lost SSO jar")
			}
			http.Redirect(w, r, "https://uis-prod-msk-comagic-api.uiscom.ru/callback", 302)
		case "uis-prod-msk-comagic-api.uiscom.ru/callback":
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "fixture-go", Path: "/"})
			fmt.Fprint(w, "ok")
		case "uis-prod-msk-comagic-api.uiscom.ru/infopin/tech-support-web-type":
			probes++
			if c, e := r.Cookie("access_token"); e != nil || c.Value != "fixture-go" {
				t.Error("Go probe token")
			}
			if failProbe {
				http.Error(w, "fixture-password fixture-csrf", 403)
				return
			}
			fmt.Fprint(w, `[{"id":1,"name":"topic"}]`)
		case "uis-prod-msk-comagic-api.uiscom.ru/infopin/tech-support":
			fmt.Fprint(w, `[{"id":123,"is_ticket":true,"context":"test"}]`)
		case "uis-prod-msk-comagic-api.uiscom.ru/infopin/tech-support-comment":
			fmt.Fprint(w, `[{"id":321,"message":"reply"}]`)
		case "app.uiscom.ru/analytics/call/call_details_data/":
			if r.Header.Get("Authorization") != "Bearer fixture-app" {
				t.Error("app token")
			}
			fmt.Fprint(w, `{"data":{"header":{"id":123},"grid":[]}}`)
		case "api.2captcha.com/createTask":
			creates++
			if r.Header.Get("Cookie") != "" {
				t.Error("cookies sent to solver")
			}
			var p struct {
				ClientKey string            `json:"clientKey"`
				Task      map[string]string `json:"task"`
			}
			json.NewDecoder(r.Body).Decode(&p)
			if p.ClientKey != "fixture-captcha-key" || len(p.Task) != 4 || p.Task["websiteURL"] != "https://sso.uiscom.ru/login" || p.Task["websiteKey"] != "public-site" || p.Task["type"] != "YandexSmartCaptchaTaskProxyless" {
				t.Error("solver metadata")
			}
			fmt.Fprint(w, `{"errorId":0,"taskId":42}`)
		case "api.2captcha.com/getTaskResult":
			fmt.Fprint(w, `{"errorId":0,"status":"ready","solution":{"token":"fixture-solution"}}`)
		default:
			t.Errorf("unexpected wire request %s %s", r.Method, r.Host+r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
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
	run := func(args ...string) (string, error) {
		var err error
		out := capture(t, func() { cmd := newRootCmd(); cmd.SetArgs(append([]string{"--json"}, args...)); err = cmd.Execute() })
		for _, secret := range []string{"fixture-password", "fixture-captcha-key", "fixture-csrf", "fixture-solution", "fixture-app", "fixture-go"} {
			if strings.Contains(out, secret) || (err != nil && strings.Contains(err.Error(), secret)) {
				t.Error("secret in diagnostic")
			}
		}
		return out, err
	}
	for _, action := range []string{"login", "refresh"} {
		if _, err := run("auth", action, "--surface", "private", "--no-input"); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 2 || probes != 2 {
		t.Fatalf("creates=%d probes=%d", creates, probes)
	}
	sessionPath := filepath.Join(profile.State("fixture"), "session.json")
	before, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := portal.LoadSession(profile.State("fixture"))
	if err != nil || s.Login != "fixture@example.invalid" || s.CabinetToken != "fixture-cabinet" || s.AppToken != "fixture-app" || s.GoToken != "fixture-go" {
		t.Fatal("promoted session invalid")
	}
	if out, e := run("calls", "show", "123"); e != nil || !strings.Contains(out, "private.call_details_data") {
		t.Fatalf("private call read: %s %v", out, e)
	}
	if _, e := run("support", "ticket", "show", "123", "--live"); e != nil {
		t.Fatal(e)
	}
	if _, e := run("support", "sync"); e != nil {
		t.Fatal(e)
	}
	if _, e := run("support", "ticket", "show", "123"); e != nil {
		t.Fatal(e)
	}
	countBeforeWrite := posts
	if out, e := run("--force", "support", "ticket", "reply", "123", "--body", "test"); e != nil {
		t.Fatalf("support write should succeed after probe: %v %s", e, out)
	}
	if posts <= countBeforeWrite {
		t.Fatal("mutation not dispatched")
	}
	failProbe = true
	if _, e := run("auth", "refresh", "--surface", "private"); e == nil {
		t.Fatal("failed probe promoted")
	}
	after, _ := os.ReadFile(sessionPath)
	if string(before) != string(after) {
		t.Fatal("failed refresh changed session")
	}
	failProbe = false
	badRedirect = true
	if _, e := run("auth", "refresh", "--surface", "private"); e == nil {
		t.Fatal("untrusted redirect accepted")
	}
	badRedirect = false
	withoutCaptcha := a
	withoutCaptcha.CredentialProcess.Command = []string{"sh", "-c", `printf '%s' '{"schemaVersion":"uis.credential-process/v1","fields":{"portal.login":"fixture@example.invalid","portal.password":"fixture-password"}}'`}
	if e := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": withoutCaptcha}}); e != nil {
		t.Fatal(e)
	}
	captchaCount := creates
	if _, e := run("auth", "refresh", "--surface", "private", "--no-input"); e == nil {
		t.Fatal("missing captcha key accepted")
	}
	if creates != captchaCount {
		t.Fatal("missing key created paid task")
	}
	after, _ = os.ReadFile(sessionPath)
	if string(before) != string(after) {
		t.Fatal("missing key changed session")
	}

	a.ExpectedLogin = "other@example.invalid"
	profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": a}})
	count := posts
	if _, e := run("auth", "refresh", "--surface", "private"); e == nil {
		t.Fatal("mismatch accepted")
	}
	if posts != count {
		t.Fatal("mismatch reached network")
	}
	after, _ = os.ReadFile(sessionPath)
	if string(before) != string(after) {
		t.Fatal("mismatch changed session")
	}
}

func TestPrivateHelpAndDryRunOffline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", dir)
	marker := filepath.Join(dir, "helper-ran")
	a := profile.Account{ExpectedLogin: "fixture"}
	a.CredentialProcess.Command = []string{"touch", marker}
	if e := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": a}}); e != nil {
		t.Fatal(e)
	}
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("offline command used network"); return nil, nil })
	for _, args := range [][]string{{"auth", "login", "--help"}, {"auth", "refresh", "--help"}, {"--dry-run", "auth", "refresh", "--surface", "private"}, {"--dry-run", "auth", "login", "--surface", "private"}} {
		capture(t, func() {
			cmd := newRootCmd()
			cmd.SetArgs(args)
			if e := cmd.Execute(); e != nil {
				t.Fatal(e)
			}
		})
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("helper started")
	}
}
