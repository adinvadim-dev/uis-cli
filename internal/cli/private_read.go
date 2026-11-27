package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

// noAutoRefreshEnv disables the one automatic private-session refresh for reads.
const noAutoRefreshEnv = "UIS_NO_AUTO_REFRESH"

// refreshBackoff pauses the automatic refresh after one failed. Every attempt
// reads the credential vault (rate-limited) and may buy a captcha; parallel or
// repeated reads after a provider-side login change must not burn both.
// An explicit `auth refresh` always runs and clears the marker on success.
const refreshBackoff = 10 * time.Minute

func refreshFailedMarker(name string) string {
	return filepath.Join(profile.State(name), "auto-refresh-failed")
}

var exitCodeNames = map[int]string{1: "execution_failed", 2: "invalid_usage", 3: "authentication_required", 4: "permission_denied", 5: "not_found", 6: "provider_error", 7: "capability_unavailable", 8: "rate_limited", 9: "outcome_unknown", 10: "confirmation_required"}

// privateRead runs a read-only private operation with one self-heal: when the
// stored session is expired, missing or unloadable and the account has a
// credential_process, it performs the same refresh as
// `auth refresh --surface private --no-input` and retries the read once.
// Mutations must keep using privateClient directly: they never refresh or replay.
func privateRead(ctx context.Context, g *globalFlags, read func(*portal.Client) error) error {
	c, e := privateClient(g)
	return retryPrivateRead(ctx, g, c, e, read)
}

// retryPrivateRead is privateRead for callers that already opened the client
// (for example the auto-source selector). openErr is the opening failure, if any.
func retryPrivateRead(ctx context.Context, g *globalFlags, c *portal.Client, openErr error, read func(*portal.Client) error) error {
	first := openErr
	if first == nil {
		first = operationError(read(c))
	}
	if first == nil || !refreshableAuthError(first) {
		return first
	}
	name, account, e := profileFor(g)
	if e != nil || !autoRefreshAvailable(account) {
		return first
	}
	marker := refreshFailedMarker(name)
	if st, err := os.Stat(marker); err == nil && time.Since(st.ModTime()) < refreshBackoff {
		return withAutoRefresh(first, "skipped_recent_failure", nil)
	}
	if g.Verbose {
		fmt.Fprintln(os.Stderr, "uis: private session rejected; refreshing it once automatically ("+noAutoRefreshEnv+"=1 disables)")
	}
	if e = refreshPrivateSession(ctx, g, name, account); e != nil {
		if os.MkdirAll(filepath.Dir(marker), 0o700) == nil {
			_ = os.WriteFile(marker, nil, 0o600)
		}
		return withAutoRefresh(first, "refresh_failed", e)
	}
	c, e = privateClient(g)
	if e == nil {
		e = operationError(read(c))
	}
	if e == nil {
		return nil
	}
	if refreshableAuthError(e) {
		return withAutoRefresh(first, "retry_failed", e)
	}
	return withAutoRefresh(e, "refreshed", nil)
}

// refreshableAuthError accepts only expired/missing private sessions: exit 3
// carrying the private recovery hint. Principal mismatch never qualifies.
func refreshableAuthError(e error) bool {
	var ee *ExitError
	if !errors.As(e, &ee) || ee.Code != 3 || ee.Details["recovery"] != privateAuthRecovery {
		return false
	}
	stage, _ := ee.Details["stage"].(string)
	return stage != "principal_mismatch" && stage != "credential_binding"
}

func autoRefreshAvailable(a profile.Account) bool {
	if v := os.Getenv(noAutoRefreshEnv); v == "1" || v == "true" {
		return false
	}
	return len(a.CredentialProcess.Command) > 0 && portal.NormalizeLogin(a.ExpectedLogin) != "" && a.ExpectedCustomerID == ""
}

// withAutoRefresh records the attempt on the returned error without changing
// its code, message contract or existing recovery details.
func withAutoRefresh(e error, outcome string, cause error) error {
	var ee *ExitError
	if !errors.As(e, &ee) {
		return e
	}
	details := map[string]interface{}{}
	for k, v := range ee.Details {
		details[k] = v
	}
	attempt := map[string]interface{}{"attempted": true, "outcome": outcome}
	if cause != nil {
		code := 1
		var ce *ExitError
		if errors.As(cause, &ce) {
			code = ce.Code
		}
		attempt["error_code"] = exitCodeNames[code]
	}
	details["auto_refresh"] = attempt
	msg := ee.Msg
	if outcome != "refreshed" {
		msg += "; automatic private refresh attempted (" + outcome + ")"
	}
	return &ExitError{Code: ee.Code, Msg: msg, Details: details}
}

// refreshPrivateSession is the non-interactive private login/refresh shared by
// `auth login|refresh --surface private` and the read self-heal. It promotes
// the staged session only after credential binding and an authenticated read.
func refreshPrivateSession(ctx context.Context, g *globalFlags, name string, a profile.Account) error {
	if portal.NormalizeLogin(a.ExpectedLogin) == "" {
		return exitf(3, "Configure expected_login before private login")
	}
	if a.ExpectedCustomerID != "" {
		return exitf(7, "Customer identity probe unavailable; previous session preserved")
	}
	to, err := timeout(g)
	if err != nil {
		return err
	}
	fields, err := profile.Resolve(ctx, a, "portal.login", "portal.password")
	if err != nil {
		return credentialError(err)
	}
	err = portal.LoginAndPromote(ctx, profile.State(name), a.ExpectedLogin, a.ExpectedCustomerID, fields["portal.login"], fields["portal.password"], to, func() (string, error) {
		f, e := profile.Resolve(ctx, a, "captcha.2captcha_api_key")
		if e != nil {
			return "", credentialError(e)
		}
		return f["captcha.2captcha_api_key"], nil
	})
	if err != nil {
		var ce *portal.CaptchaError
		if errors.As(err, &ce) {
			return exitf(6, "%s", ce)
		}
		return operationError(err)
	}
	_ = os.Remove(refreshFailedMarker(name))
	return nil
}

// credentialError keeps exit 3 but names the stage: the external credential
// source failed (for example a rate-limited vault), not the UIS login itself.
// The recovery hint is deliberately absent so reads never auto-refresh on it.
func credentialError(e error) error {
	return &ExitError{Code: 3, Msg: e.Error(), Details: map[string]interface{}{"stage": "credential_process"}}
}
