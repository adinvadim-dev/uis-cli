// Package profile owns non-secret profiles and lazy external credentials.
package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Account struct {
	Alias              string `toml:"alias"`
	ExpectedLogin      string `toml:"expected_login"`
	ExpectedCustomerID string `toml:"expected_customer_id,omitempty"`
	CredentialProcess  struct {
		Command []string `toml:"command"`
	} `toml:"credential_process"`
}
type Config struct {
	DefaultAccount string             `toml:"default_account"`
	Accounts       map[string]Account `toml:"accounts"`
}

var ValidName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func Path() string {
	if p := os.Getenv("UIS_CONFIG"); p != "" {
		return p
	}
	p := os.Getenv("XDG_CONFIG_HOME")
	if p == "" {
		h, _ := os.UserHomeDir()
		p = filepath.Join(h, ".config")
	}
	return filepath.Join(p, "uis", "config.toml")
}
func Load(path string) (Config, error) {
	c := Config{Accounts: map[string]Account{}}
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	e = toml.Unmarshal(b, &c)
	return c, e
}
func Save(path string, c Config) error {
	b, e := toml.Marshal(c)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".profile-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func Select(c Config, name string) (string, Account, error) {
	if name == "" {
		name = os.Getenv("UIS_ACCOUNT")
	}
	if name == "" {
		name = c.DefaultAccount
	}
	if name == "" {
		name = "default"
	}
	if !ValidName.MatchString(name) {
		return "", Account{}, errors.New("invalid account name")
	}
	a := c.Accounts[name]
	return name, a, nil
}
func State(name string) string {
	p := os.Getenv("XDG_STATE_HOME")
	if p == "" {
		h, _ := os.UserHomeDir()
		p = filepath.Join(h, ".local", "state")
	}
	return filepath.Join(p, "uis", "accounts", name)
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, errors.New("credential output too large")
	}
	return b.Buffer.Write(p)
}
func Resolve(ctx context.Context, a Account, fields ...string) (map[string]string, error) {
	out := map[string]string{}
	need := false
	for _, f := range fields {
		if f == "dataapi.access_token" && os.Getenv("UIS_TOKEN") != "" {
			out[f] = os.Getenv("UIS_TOKEN")
		} else {
			need = true
		}
	}
	if !need {
		return out, nil
	}
	argv := a.CredentialProcess.Command
	if len(argv) == 0 || len(argv) > 64 {
		return nil, errors.New("configure credential_process for required credentials")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var b limitedBuffer
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return nil, errors.New("credential_process failed (output suppressed)")
	}
	var doc struct {
		Schema string                     `json:"schemaVersion"`
		Fields map[string]json.RawMessage `json:"fields"`
	}
	d := json.NewDecoder(&b)
	if d.Decode(&doc) != nil || doc.Schema != "uis.credential-process/v1" {
		return nil, errors.New("invalid credential_process document")
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("credential_process must emit one document")
	}
	for _, f := range fields {
		if out[f] != "" {
			continue
		}
		var value string
		if json.Unmarshal(doc.Fields[f], &value) != nil || value == "" {
			return nil, errors.New("credential field unavailable: " + f)
		}
		out[f] = value
	}
	return out, nil
}
