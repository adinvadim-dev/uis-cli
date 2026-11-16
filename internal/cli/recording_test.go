package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const freshSession = `{"login":"fixture@example.invalid","cabinet_token":"fresh-cabinet","go_token":"fresh-go","app_token":"fresh-app"}`

// recordingFixture serves the live-observed contract: call details carry
// talk_file "<record>:<seconds>" entries, the report constructor's call_records
// cell carries self-authorizing media links, and media is plain audio.
type recordingFixture struct {
	*refreshFixture
	details, media int
	headerID       string
	audio          map[string]string // locator token -> bytes
}

func newRecordingFixture(t *testing.T, talkFiles []string, links []string) *recordingFixture {
	f := &recordingFixture{refreshFixture: newRefreshFixture(t), headerID: "123", audio: map[string]string{}}
	f.writeSession(freshSession)
	f.routes["app.uiscom.ru/analytics/call/call_details_data/"] = func(w http.ResponseWriter, r *http.Request) {
		f.details++
		if r.Method != "GET" || r.URL.Query().Get("call_id") != "123" {
			t.Errorf("unexpected details request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer fresh-app" {
			http.Error(w, "expired", 401)
			return
		}
		tf, _ := json.Marshal(talkFiles)
		fmt.Fprintf(w, `{"success":true,"data":{"header":{"id":%s,"start_time":"2026-11-13T11:58:26","talk_file":%s},"grid":[]}}`, f.headerID, tf)
	}
	cell := "null"
	if links != nil {
		b, _ := json.Marshal(map[string]interface{}{"links": links, "links_name": []string{"fixture.mp3"}})
		cell = string(b)
	}
	f.reportBody = `{"data":[{"communication_id":123,"call_records":` + cell + `}],"metadata":{"total_items":1}}`
	for i, l := range links {
		token := fmt.Sprintf("locator-secret-%d", i+1)
		f.audio[token] = fmt.Sprintf("ID3 fixture audio leg %d", i+1)
		path := strings.TrimPrefix(l, "https://")
		body := f.audio[token]
		f.routes[path] = func(w http.ResponseWriter, r *http.Request) {
			f.media++
			if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Errorf("media request must be a credential-free GET")
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			fmt.Fprint(w, body)
		}
	}
	return f
}

func link(id, token string) string {
	return "https://app.comagic.ru/system/media/talk/" + id + "/" + token + "/"
}

func (f *recordingFixture) download(args ...string) refreshResult {
	f.t.Helper()
	r := f.run(append([]string{"recording", "download", "123"}, args...)...)
	for _, leak := range []string{"locator-secret", "system/media", "comagic", "https://"} {
		if strings.Contains(r.out, leak) {
			f.t.Fatalf("recording locator leaked (%s): %s", leak, r.out)
		}
	}
	return r
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestRecordingDownloadWritesBytesAndMetadataOnly(t *testing.T) {
	t.Run("single_leg", func(t *testing.T) {
		f := newRecordingFixture(t, []string{"4440643401:453"}, []string{link("123", "locator-secret-1")})
		dir := t.TempDir()
		out := filepath.Join(dir, "call.mp3")
		r := f.download("--output", out)
		if r.code != 0 {
			t.Fatalf("exit=%d %s", r.code, r.out)
		}
		got, err := os.ReadFile(out)
		if err != nil || string(got) != f.audio["locator-secret-1"] {
			t.Fatalf("bytes not written: %q %v", got, err)
		}
		if st, _ := os.Stat(out); st.Mode().Perm() != 0600 {
			t.Fatalf("recording must be private, mode %v", st.Mode().Perm())
		}
		if names := dirEntries(t, dir); len(names) != 1 {
			t.Fatalf("created extra files: %v", names)
		}
		var env struct {
			Data struct {
				CommunicationID string                   `json:"communication_id"`
				Source          string                   `json:"source"`
				Files           []map[string]interface{} `json:"files"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(r.out), &env); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(got)
		file := env.Data.Files[0]
		if env.Data.CommunicationID != "123" || env.Data.Source == "" || len(env.Data.Files) != 1 || file["path"] != out || file["bytes"] != float64(len(got)) || file["sha256"] != hex.EncodeToString(sum[:]) || file["content_type"] != "audio/mpeg" || file["leg"] != float64(1) {
			t.Fatalf("metadata: %s", r.out)
		}
		if f.media != 1 || f.posts != 1 {
			t.Fatalf("media=%d report posts=%d", f.media, f.posts)
		}
	})

	t.Run("multiple_legs_get_suffixes", func(t *testing.T) {
		f := newRecordingFixture(t, []string{"1:10", "2:20"}, []string{link("123", "locator-secret-1"), link("123", "locator-secret-2")})
		dir := t.TempDir()
		r := f.download("--output", filepath.Join(dir, "call.mp3"))
		if r.code != 0 {
			t.Fatalf("exit=%d %s", r.code, r.out)
		}
		for i, name := range []string{"call.leg1.mp3", "call.leg2.mp3"} {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || string(b) != f.audio[fmt.Sprintf("locator-secret-%d", i+1)] {
				t.Fatalf("%s: %q %v", name, b, err)
			}
		}
		if names := dirEntries(t, dir); len(names) != 2 {
			t.Fatalf("unexpected files: %v", names)
		}
	})

	t.Run("explicit_leg_writes_exact_path", func(t *testing.T) {
		f2 := newRecordingFixture(t, []string{"1:10", "2:20"}, []string{link("123", "locator-secret-1"), link("123", "locator-secret-2")})
		one := filepath.Join(t.TempDir(), "only.mp3")
		if r := f2.download("--leg", "2", "--output", one); r.code != 0 || f2.media != 1 {
			t.Fatalf("--leg 2: exit=%d media=%d %s", r.code, f2.media, r.out)
		}
		if b, _ := os.ReadFile(one); string(b) != f2.audio["locator-secret-2"] {
			t.Fatalf("--leg 2 wrote %q", b)
		}
	})
}

func TestRecordingDownloadFailsClosedOnIdentityMismatch(t *testing.T) {
	for _, tc := range []struct {
		name, headerID, linkID string
	}{
		{"detail_header", "124", "123"},
		{"media_locator", "123", "124"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecordingFixture(t, []string{"1:10"}, []string{link(tc.linkID, "locator-secret-1")})
			f.headerID = tc.headerID
			dir := t.TempDir()
			r := f.download("--output", filepath.Join(dir, "call.mp3"))
			if r.code != 7 || f.media != 0 {
				t.Fatalf("exit=%d media=%d %s", r.code, f.media, r.out)
			}
			if names := dirEntries(t, dir); len(names) != 0 {
				t.Fatalf("wrote files on identity failure: %v", names)
			}
		})
	}
}

func TestRecordingDownloadMissingRecordingIsNotFound(t *testing.T) {
	f := newRecordingFixture(t, []string{}, nil)
	dir := t.TempDir()
	r := f.download("--output", filepath.Join(dir, "call.mp3"))
	if r.code != 5 || r.details["reason"] != "no_recording" || !strings.Contains(fmt.Sprint(r.details["hint"]), "no downloadable recording") {
		t.Fatalf("exit=%d %s", r.code, r.out)
	}
	if names := dirEntries(t, dir); len(names) != 0 || f.media != 0 {
		t.Fatalf("files=%v media=%d", names, f.media)
	}
}

func TestRecordingDownloadMissingLegIsNotFound(t *testing.T) {
	f := newRecordingFixture(t, []string{"1:10"}, []string{link("123", "locator-secret-1")})
	dir := t.TempDir()
	if r := f.download("--leg", "2", "--output", filepath.Join(dir, "call.mp3")); r.code != 5 || r.details["reason"] != "leg_not_found" || f.media != 0 {
		t.Fatalf("missing leg: exit=%d %s", r.code, r.out)
	}
	if names := dirEntries(t, dir); len(names) != 0 {
		t.Fatalf("files=%v", names)
	}
}

func TestRecordingDownloadRefusesOverwriteWithoutForce(t *testing.T) {
	f := newRecordingFixture(t, []string{"1:10"}, []string{link("123", "locator-secret-1")})
	out := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(out, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	r := f.download("--output", out)
	if b, _ := os.ReadFile(out); r.code != 10 || r.details["reason"] != "output_exists" || string(b) != "keep me" || f.media != 0 {
		t.Fatalf("exit=%d media=%d content=%q %s", r.code, f.media, b, r.out)
	}
	if names := dirEntries(t, filepath.Dir(out)); len(names) != 1 {
		t.Fatalf("left temp files: %v", names)
	}
	if r := f.download("--force", "--output", out); r.code != 0 {
		t.Fatalf("--force: exit=%d %s", r.code, r.out)
	}
	if b, _ := os.ReadFile(out); string(b) != f.audio["locator-secret-1"] {
		t.Fatalf("--force did not overwrite: %q", b)
	}
}

func TestRecordingDownloadRefreshesExpiredSessionOnce(t *testing.T) {
	f := newRecordingFixture(t, []string{"1:10"}, []string{link("123", "locator-secret-1")})
	f.writeSession(staleSession)
	out := filepath.Join(t.TempDir(), "call.mp3")
	r := f.download("--output", out)
	if r.code != 0 || f.logins != 1 || f.details != 2 || f.media != 1 {
		t.Fatalf("exit=%d logins=%d details=%d media=%d %s", r.code, f.logins, f.details, f.media, r.out)
	}
	if b, _ := os.ReadFile(out); string(b) != f.audio["locator-secret-1"] {
		t.Fatalf("bytes after refresh: %q", b)
	}
}
