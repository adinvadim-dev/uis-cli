package profile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialProcessStrictDocumentAndEnvPrecedence(t *testing.T) {
	for _, test := range []struct {
		name, doc string
		ok        bool
	}{{"valid", `{"schemaVersion":"uis.credential-process/v1","fields":{"dataapi.access_token":"fixture"}}`, true}, {"unknown_field", `{"schemaVersion":"uis.credential-process/v1","fields":{"dataapi.access_token":"fixture","unknown":{"nested":true}}}`, true},
		{"trailing", `{"schemaVersion":"uis.credential-process/v1","fields":{"dataapi.access_token":"fixture"}} {}`, false}, {"wrong_schema", `{"schemaVersion":"wrong","fields":{"dataapi.access_token":"fixture"}}`, false}, {"missing", `{"schemaVersion":"uis.credential-process/v1","fields":{}}`, false}} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("UIS_TOKEN", "")
			dir := t.TempDir()
			p := filepath.Join(dir, "helper")
			if e := os.WriteFile(p, []byte("#!/bin/sh\ncat <<'DOC'\n"+test.doc+"\nDOC\n"), 0700); e != nil {
				t.Fatal(e)
			}
			a := Account{}
			a.CredentialProcess.Command = []string{p}
			v, e := Resolve(context.Background(), a, "dataapi.access_token")
			if (e == nil) != test.ok {
				t.Fatalf("%v", e)
			}
			if test.ok && v["dataapi.access_token"] != "fixture" {
				t.Fatal("wrong credential")
			}
		})
	}
	t.Setenv("UIS_TOKEN", "env-fixture")
	a := Account{}
	a.CredentialProcess.Command = []string{"/does/not/exist"}
	v, e := Resolve(context.Background(), a, "dataapi.access_token")
	if e != nil || v["dataapi.access_token"] != "env-fixture" {
		t.Fatal("env did not override helper")
	}
}
func TestAccountTraversalRejected(t *testing.T) {
	for _, s := range []string{"../other", "a/b", ".", "../../session"} {
		if _, _, e := Select(Config{}, s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
