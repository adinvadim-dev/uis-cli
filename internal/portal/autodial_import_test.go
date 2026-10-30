package portal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutodialImportMultipartDoesNotSaveCampaign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts.csv")
	raw := "numa;name\n70000000000;fixture\n"
	if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/auto_call/campaign/import_numbers" {
			t.Fatal("import must not save or start a campaign")
		}
		if r.Header.Get("Authorization") != "Bearer fixture-jwt" {
			t.Fatal("wrong import session")
		}
		if r.ParseMultipartForm(1<<20) != nil {
			t.Fatal("invalid multipart")
		}
		defer r.MultipartForm.RemoveAll()
		var old []Object
		if json.Unmarshal([]byte(r.FormValue("old_numbers")), &old) != nil || len(old) != 1 || old[0]["numa"] != "70000000001" {
			t.Fatal("lost existing editor rows")
		}
		f, h, e := r.FormFile("file")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		if h.Filename != "contacts.csv" || string(b) != raw {
			t.Fatal("file changed")
		}
		io.WriteString(w, `{"numbers":[{"numa":"70000000000"}]}`)
	}))
	defer s.Close()
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	c.SupervisorURL = s.URL
	_, e := c.Autodial(context.Background(), "import", "", Object{"input_file": path, "old_numbers": []Object{{"numa": "70000000001"}}})
	if e != nil || calls != 1 {
		t.Fatalf("import failed or replayed: %v", e)
	}
}
