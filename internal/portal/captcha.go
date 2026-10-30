package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// CaptchaError deliberately excludes provider text and request/response bodies.
// Ambiguous create outcomes must never be retried automatically.
type CaptchaError struct {
	Stage     string
	Ambiguous bool
}

func (e *CaptchaError) Error() string {
	return "2Captcha failed at " + e.Stage + "; task was not recreated"
}

type Captcha struct {
	HTTP         *http.Client
	PollInterval time.Duration
}

func NewCaptcha(transport http.RoundTripper) *Captcha {
	return &Captcha{HTTP: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, PollInterval: 5 * time.Second}
}

type captchaResult struct {
	ErrorID  *int   `json:"errorId"`
	TaskID   int64  `json:"taskId"`
	Status   string `json:"status"`
	Solution struct {
		Token string `json:"token"`
	} `json:"solution"`
}

func (c *Captcha) call(ctx context.Context, path string, payload interface{}) (captchaResult, error) {
	var out captchaResult
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.2captcha.com/"+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return out, &CaptchaError{Stage: path + "_transport", Ambiguous: path == "createTask"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, &CaptchaError{Stage: path + "_http", Ambiguous: path == "createTask"}
	}
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&out) != nil || out.ErrorID == nil {
		return out, &CaptchaError{Stage: path + "_schema", Ambiguous: path == "createTask"}
	}
	if *out.ErrorID != 0 {
		return out, &CaptchaError{Stage: path + "_provider"}
	}
	return out, nil
}
func (c *Captcha) Solve(ctx context.Context, key, page, siteKey string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if key == "" {
		return "", &CaptchaError{Stage: "missing_key"}
	}
	task, e := c.call(ctx, "createTask", map[string]interface{}{"clientKey": key, "task": map[string]string{"type": "YandexSmartCaptchaTaskProxyless", "websiteURL": page, "websiteKey": siteKey, "userAgent": userAgent}})
	if e != nil {
		return "", e
	}
	if task.TaskID <= 0 {
		return "", &CaptchaError{Stage: "createTask_identity", Ambiguous: true}
	}
	for {
		timer := time.NewTimer(c.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", &CaptchaError{Stage: "poll_deadline"}
		case <-timer.C:
		}
		r, e := c.call(ctx, "getTaskResult", map[string]interface{}{"clientKey": key, "taskId": task.TaskID})
		if e != nil {
			return "", e
		}
		switch r.Status {
		case "processing":
			continue
		case "ready":
			if r.Solution.Token != "" {
				return r.Solution.Token, nil
			}
		}
		return "", &CaptchaError{Stage: "result_schema"}
	}
}
