package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

func TestAutodialRPCUsesPrivateSessionWithoutKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("UIS_TOKEN", "")
	a := profile.Account{ExpectedLogin: "fixture@example.invalid"}
	a.CredentialProcess.Command = []string{"/must/not/run"}
	if e := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": a}}); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(profile.State("fixture"), 0700); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(portal.Session{Login: a.ExpectedLogin, CabinetToken: "fixture-jwt"})
	if e := os.WriteFile(filepath.Join(profile.State("fixture"), "session.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "dataapi.uiscom.ru" || r.Header.Get("Authorization") != "Bearer fixture-jwt" {
			t.Fatal("wrong auth surface")
		}
		var v portal.Object
		json.NewDecoder(r.Body).Decode(&v)
		if v["method"] != "get.auto_call_campaigns" {
			t.Fatal("wrong method")
		}
		if _, ok := v["params"].(map[string]interface{})["access_token"]; ok {
			t.Fatal("key parameter sent")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"uis-autodial","result":{"data":[{"id":9007199254740993}],"metadata":{}}}`))}, nil
	})
	for _, source := range []string{"auto", "private"} {
		for _, route := range [][]string{{"auto-call-campaigns", "list"}, {"call", "get.auto_call_campaigns"}} {
			out := capture(t, func() {
				r := newRootCmd()
				r.SetArgs(append([]string{"--json"}, append(route, "--source", source, "--param", "limit:=1")...))
				if e := r.Execute(); e != nil {
					t.Fatal(e)
				}
			})
			if !strings.Contains(out, `"source":"private.cabinet_rpc"`) || !strings.Contains(out, "9007199254740993") {
				t.Fatal("missing provenance or integer fidelity")
			}
		}
	}
	if calls != 4 {
		t.Fatal("unexpected read count")
	}
}
