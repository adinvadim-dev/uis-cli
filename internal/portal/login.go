package portal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

const appOrigin = "https://app.uiscom.ru"
const goOrigin = "https://uis-prod-msk-comagic-api.uiscom.ru"
const userAgent = "Mozilla/5.0"

// NormalizeLogin matches login labels case-insensitively; passwords are untouched.
func NormalizeLogin(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// LoginAndPromote binds credentials to a successful read, not an independently
// reported provider identity. No write verification callback is installed.
func LoginAndPromote(ctx context.Context, dir, expected, customer, login, password string, timeout time.Duration, captchaKey func() (string, error)) error {
	if NormalizeLogin(expected) == "" || NormalizeLogin(login) != NormalizeLogin(expected) {
		return &Error{Code: 3, Stage: "credential_binding"}
	}
	// There is no observed endpoint to verify a customer ID.
	if customer != "" {
		return &Error{Code: 7, Stage: "customer_probe_unavailable"}
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	h := &http.Client{Jar: jar, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	l := loginFlow{http: h, key: captchaKey}
	b, _, err := l.request(ctx, "POST", "https://www.uiscom.ru/api/", url.Values{"method": {"login"}, "params[email]": {login}, "params[password]": {password}, "params[token]": {"0token"}})
	if err != nil {
		return err
	}
	var legacy struct {
		Success  bool   `json:"success"`
		Redirect string `json:"redirect_url"`
	}
	if json.Unmarshal(b, &legacy) != nil || !legacy.Success {
		return &Error{Code: 3, Stage: "legacy_login"}
	}
	next := legacy.Redirect
	if next == "" {
		next = appOrigin
	}
	app, err := l.follow(ctx, next, appOrigin, login, password, true)
	if err != nil {
		return err
	}
	goToken, err := l.follow(ctx, goOrigin+"/billing/service", goOrigin, login, password, false)
	if err != nil {
		return err
	}
	s := Session{AppToken: app, GoToken: goToken}
	return Promote(dir, s, NormalizeLogin(expected), func(s Session) (string, error) {
		c := New(s, timeout)
		v, err := c.Read(ctx, "tech-support-web-type", nil)
		if err != nil {
			return "", err
		}
		if _, err = Rows(v); err != nil {
			return "", err
		}
		return NormalizeLogin(login), nil
	})
}

type loginFlow struct {
	http *http.Client
	key  func() (string, error)
}

func trusted(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Host {
	case "www.uiscom.ru", "app.uiscom.ru", "sso.uiscom.ru", "go.uiscom.ru", "uis-prod-msk-comagic-api.uiscom.ru":
		return true
	}
	return false
}
func (l *loginFlow) request(ctx context.Context, method, raw string, form url.Values) ([]byte, string, error) {
	if !trusted(raw) {
		return nil, "", &Error{Code: 7, Stage: "untrusted_redirect"}
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r, e := http.NewRequestWithContext(ctx, method, raw, body)
	if e != nil {
		return nil, "", &Error{Code: 7, Stage: "login_url"}
	}
	r.Header.Set("User-Agent", userAgent)
	r.Header.Set("Referer", raw)
	if raw == "https://www.uiscom.ru/api/" {
		r.Header.Set("Referer", "https://www.uiscom.ru/user/#login")
	}
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	resp, e := l.http.Do(r)
	if e != nil {
		return nil, "", &Error{Code: 6, Stage: "login_transport"}
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if e != nil || len(b) > 2<<20 {
		return nil, "", &Error{Code: 7, Stage: "login_response"}
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc, e := resp.Location()
		if e != nil {
			return nil, "", &Error{Code: 7, Stage: "login_redirect"}
		}
		return b, loc.String(), nil
	}
	if resp.StatusCode != 200 {
		code := 6
		switch resp.StatusCode {
		case 401:
			code = 3
		case 403:
			code = 4
		case 429:
			code = 8
		}
		return nil, "", &Error{Code: code, Stage: "login_http"}
	}
	return b, "", nil
}

var appData = regexp.MustCompile(`(?:window\.)?APP_DATA\s*=\s*`)

type ssoData struct {
	Project string `json:"project"`
	Form    struct {
		CSRF       string      `json:"csrf"`
		Identity   string      `json:"identity_provider"`
		NotTrusted interface{} `json:"not_trusted_device"`
	} `json:"form_data"`
	Captcha struct {
		Type    string `json:"type"`
		SiteKey string `json:"site_key"`
	} `json:"captcha"`
}

func (l *loginFlow) follow(ctx context.Context, next, origin, login, password string, allowCaptcha bool) (string, error) {
	for i := 0; i < 8; i++ {
		current := next
		b, loc, e := l.request(ctx, "GET", current, nil)
		if e != nil {
			return "", e
		}
		if token := l.token(origin); token != "" {
			return token, nil
		}
		if loc != "" {
			next = loc
			continue
		}
		u, _ := url.Parse(current)
		m := appData.FindIndex(b)
		if !allowCaptcha || u.Host != "sso.uiscom.ru" || m == nil {
			break
		}
		var d ssoData
		if json.NewDecoder(strings.NewReader(string(b[m[1]:]))).Decode(&d) != nil || d.Form.CSRF == "" || d.Captcha.Type != "yandex" || d.Captcha.SiteKey == "" {
			return "", &Error{Code: 7, Stage: "sso_schema"}
		}
		nt := "false"
		switch v := d.Form.NotTrusted.(type) {
		case bool:
			if v {
				nt = "true"
			}
		case string:
			nt = v
		case nil:
		default:
			return "", &Error{Code: 7, Stage: "sso_schema"}
		}
		key, e := l.key()
		if e != nil {
			return "", e
		}
		// The solver sees public metadata only: strip query, fragment and user info.
		publicURL := u.Scheme + "://" + u.Host + u.Path
		solver := NewCaptcha(l.http.Transport)
		token, e := solver.Solve(ctx, key, publicURL, d.Captcha.SiteKey)
		if e != nil {
			return "", e
		}

		_, loc, e = l.request(ctx, "POST", current, url.Values{"username": {login}, "password": {password}, "project": {d.Project}, "csrf": {d.Form.CSRF}, "identity_provider": {d.Form.Identity}, "not_trusted_device": {nt}, "smart-token": {token}})
		if e != nil {
			return "", e
		}
		if token := l.token(origin); token != "" {
			return token, nil
		}
		if loc == "" {
			return "", &Error{Code: 3, Stage: "sso_login"}
		}
		next = loc
		allowCaptcha = false
	}
	return "", &Error{Code: 7, Stage: "token_not_issued"}
}
func (l *loginFlow) token(origin string) string {
	u, _ := url.Parse(origin + "/")
	for _, c := range l.http.Jar.Cookies(u) {
		if c.Name == "access_token" && c.Value != "" && c.Value != `""` {
			return c.Value
		}
	}
	return ""
}
