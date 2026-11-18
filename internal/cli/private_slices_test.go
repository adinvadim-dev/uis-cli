package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

func TestSplitSliceWindows(t *testing.T) {
	day, err := splitSliceWindows("2026-11-17 00:00:00", "2026-11-17 23:59:59", "1h")
	if err != nil || len(day) != 24 || day[0] != (sliceWindow{"2026-11-17 00:00:00", "2026-11-17 00:59:59"}) || day[23] != (sliceWindow{"2026-11-17 23:00:00", "2026-11-17 23:59:59"}) {
		t.Fatalf("whole day by hour: %v %v", day, err)
	}
	night, err := splitSliceWindows("2026-11-17 22:30:00", "2026-11-18 01:15:00", "1h")
	want := []sliceWindow{{"2026-11-17 22:30:00", "2026-11-17 23:29:59"}, {"2026-11-17 23:30:00", "2026-11-18 00:29:59"}, {"2026-11-18 00:30:00", "2026-11-18 01:15:00"}}
	if err != nil || !reflect.DeepEqual(night, want) {
		t.Fatalf("midnight crossing / capped last window: %v %v", night, err)
	}
	point, err := splitSliceWindows("2026-11-17 10:00:00", "2026-11-17 10:00:00", "90m")
	if err != nil || len(point) != 1 || point[0].Till != "2026-11-17 10:00:00" {
		t.Fatalf("single-second window: %v %v", point, err)
	}
	if month, err := splitSliceWindows("2026-10-01 00:00:00", "2026-10-31 23:59:59", "1h"); err != nil || len(month) != 744 {
		t.Fatalf("a 31-day month of hours must fit the cap: %d %v", len(month), err)
	}
	for _, tc := range []struct{ from, till, slice string }{
		{"2026-11-17 00:00:00", "2026-11-17 23:59:59", "30s"},
		{"2026-11-17 00:00:00", "2026-11-17 23:59:59", "90s"},
		{"2026-11-17 00:00:00", "2026-11-17 23:59:59", "0"},
		{"2026-11-17 00:00:00", "2026-11-17 23:59:59", "-1h"},
		{"2026-11-17 00:00:00", "2026-11-17 23:59:59", "hour"},
		{"2026-11-17 00:00:00", "2026-11-17 23:59:59", "1m"}, // 1440 windows
		{"2026-11-17 00:00:00", "2026-11-16 23:59:59", "1h"},
		{"2026-11-17", "2026-11-17 23:59:59", "1h"},
	} {
		var ee *ExitError
		if _, err := splitSliceWindows(tc.from, tc.till, tc.slice); !errors.As(err, &ee) || ee.Code != 2 {
			t.Fatalf("%+v: want exit 2, got %v", tc, err)
		}
	}
}

func TestSliceFlagValidatesBeforeCredentials(t *testing.T) {
	t.Setenv("UIS_CONFIG", "/must/not/read.toml")
	window := []string{"--from", "2026-11-17 00:00:00", "--till", "2026-11-17 23:59:59"}
	for _, args := range [][]string{
		append([]string{"--source", "private", "--slice", "1h"}, window...),
		append([]string{"--all", "--slice", "1h"}, window...),
		append([]string{"--source", "dataapi", "--all", "--slice", "1h"}, window...),
		append([]string{"--source", "private", "--all", "--slice", "45s"}, window...),
		append([]string{"--source", "private", "--all", "--slice", "1m"}, window...),
		append([]string{"--source", "private", "--all", "--slice", "1h", "--offset", "5"}, window...),
	} {
		root := newRootCmd()
		root.SetArgs(append([]string{"--dry-run", "calls", "search"}, args...))
		var ee *ExitError
		if err := root.Execute(); !errors.As(err, &ee) || ee.Code != 2 {
			t.Fatalf("%v: expected local usage error, got %v", args, err)
		}
	}
	out := capture(t, func() {
		root := newRootCmd()
		root.SetArgs(append([]string{"--json", "--dry-run", "calls", "search", "--source", "private", "--all", "--slice", "2h"}, window...))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"windows":[{"from":"2026-11-17 00:00:00","till":"2026-11-17 01:59:59"}`) {
		t.Fatalf("dry run must show the windows: %s", out)
	}
}

// sliceFixture serves one report-constructor page per request, chosen by the
// window (date_from) and the attempt number for that window.
func sliceFixture(t *testing.T, serve func(q portal.CallQuery, attempt int) (string, error)) map[string]int {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("UIS_TOKEN", "")
	if err := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": {ExpectedLogin: "fixture@example.invalid"}}}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(profile.State("fixture"), 0700)
	os.WriteFile(filepath.Join(profile.State("fixture"), "session.json"), []byte(`{"login":"fixture@example.invalid","cabinet_token":"fixture"}`), 0600)
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	attempts := map[string]int{}
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var q portal.CallQuery
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Fatal(err)
		}
		if q.Offset == 0 {
			attempts[q.From]++
		}
		body, err := serve(q, attempts[q.From])
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	return attempts
}

func page(total interface{}, ids ...int) string {
	rows := []portal.Object{}
	for _, id := range ids {
		rows = append(rows, portal.Object{"communication_id": id})
	}
	meta := portal.Object{}
	if total != nil {
		meta["total_items"] = total
	}
	b, _ := json.Marshal(portal.Object{"data": rows, "metadata": meta})
	return string(b)
}

type sliceEnvelope struct {
	Data struct {
		Source     string                   `json:"source"`
		DateFrom   string                   `json:"date_from"`
		DateTill   string                   `json:"date_till"`
		Rows       []map[string]interface{} `json:"data"`
		QueryScope map[string]interface{}   `json:"query_scope"`
		Coverage   struct {
			Complete   bool   `json:"complete"`
			Reason     string `json:"reason"`
			Returned   int    `json:"returned"`
			Reported   *int   `json:"reported_total"`
			Windows    int    `json:"windows"`
			Empty      int    `json:"empty_windows"`
			Retried    int    `json:"retried_windows"`
			Duplicates int    `json:"duplicates_removed"`
			Slices     []struct {
				From     string `json:"from"`
				Till     string `json:"till"`
				Complete bool   `json:"complete"`
				Reason   string `json:"reason"`
				Returned int    `json:"returned"`
				Pages    int    `json:"pages"`
				Attempts int    `json:"attempts"`
			} `json:"slices"`
		} `json:"coverage"`
		Observation map[string]interface{} `json:"observation"`
		Limitations []string               `json:"limitations"`
	} `json:"data"`
	Error struct {
		Details map[string]interface{} `json:"details"`
	} `json:"error"`
}

var sliceSearch = []string{"--json", "calls", "search", "--source", "private", "--analysis", "--all", "--slice", "1h", "--limit", "10", "--from", "2026-11-17 10:00:00", "--till", "2026-11-17 12:59:59"}

func runSlices(t *testing.T, args ...string) (int, sliceEnvelope, string) {
	t.Helper()
	var code int
	out := capture(t, func() { code = Run(args) })
	var env sliceEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("bad JSON: %s", out)
	}
	return code, env, out
}

func TestPrivateSliceMergesWindows(t *testing.T) {
	sliceFixture(t, func(q portal.CallQuery, _ int) (string, error) {
		switch q.From {
		case "2026-11-17 10:00:00":
			if q.Till != "2026-11-17 10:59:59" {
				t.Fatalf("window not closed at the hour: %s", q.Till)
			}
			return page(2, 1, 2), nil
		case "2026-11-17 11:00:00":
			return page(nil), nil // UIS sends no total for an empty set
		default:
			return page(2, 2, 3), nil
		}
	})
	code, env, out := runSlices(t, sliceSearch...)
	if code != 0 {
		t.Fatal(out)
	}
	d, cov := env.Data, env.Data.Coverage
	if d.Source != "private.report_constructor" || d.DateFrom != "2026-11-17 10:00:00" || d.DateTill != "2026-11-17 12:59:59" || d.QueryScope["slice"] != "1h" || d.Observation["started_at"] == nil || len(d.Limitations) == 0 {
		t.Fatalf("top-level shape changed: %s", out)
	}
	ids := []interface{}{}
	for _, row := range d.Rows {
		ids = append(ids, row["communication_id"])
	}
	if !reflect.DeepEqual(ids, []interface{}{float64(1), float64(2), float64(3)}) {
		t.Fatalf("rows must be concatenated in window order and deduplicated: %v", ids)
	}
	if !cov.Complete || cov.Reason != "slices_complete" || cov.Returned != 3 || cov.Reported == nil || *cov.Reported != 4 || cov.Windows != 3 || cov.Empty != 1 || cov.Retried != 0 || cov.Duplicates != 1 || len(cov.Slices) != 3 {
		t.Fatalf("coverage: %s", out)
	}
	if s := cov.Slices[1]; s.From != "2026-11-17 11:00:00" || s.Till != "2026-11-17 11:59:59" || s.Complete || s.Reason != "provider_count_unavailable" || s.Returned != 0 || s.Pages != 1 {
		t.Fatalf("empty slice: %+v", s)
	}
}

func TestPrivateSliceIncompleteWindowAndUnknownTotal(t *testing.T) {
	sliceFixture(t, func(q portal.CallQuery, _ int) (string, error) {
		if q.From == "2026-11-17 11:00:00" {
			return page(5, q.Offset+100), nil // more rows than --max-pages 1 can read
		}
		return page(1, map[string]int{"2026-11-17 10:00:00": 1, "2026-11-17 12:00:00": 3}[q.From]), nil
	})
	code, env, out := runSlices(t, append(append([]string{}, sliceSearch...), "--limit", "1", "--max-pages", "1")...)
	if code != 0 {
		t.Fatal(out)
	}
	if cov := env.Data.Coverage; cov.Complete || cov.Reason != "slice_incomplete" || cov.Slices[1].Reason != "page_limit" || cov.Returned != 3 {
		t.Fatalf("a window cut by --max-pages must make the result incomplete: %s", out)
	}
}

func TestPrivateSliceRetriesTransientWindowOnce(t *testing.T) {
	for _, mode := range []string{"transport", "drift"} {
		t.Run(mode, func(t *testing.T) {
			attempts := sliceFixture(t, func(q portal.CallQuery, attempt int) (string, error) {
				if q.From != "2026-11-17 11:00:00" {
					return page(1, map[string]int{"2026-11-17 10:00:00": 1, "2026-11-17 12:00:00": 3}[q.From]), nil
				}
				if attempt == 1 && mode == "transport" {
					return "", fmt.Errorf("synthetic timeout")
				}
				if attempt == 1 && q.Offset == 1 {
					return page(3, 9), nil // total moved between pages
				}
				return page(2, 20+q.Offset), nil
			})
			code, env, out := runSlices(t, append(append([]string{}, sliceSearch...), "--limit", "1")...)
			if code != 0 {
				t.Fatal(out)
			}
			cov := env.Data.Coverage
			if !cov.Complete || cov.Retried != 1 || cov.Returned != 4 || cov.Slices[1].Attempts != 2 || cov.Slices[0].Attempts != 1 || attempts["2026-11-17 11:00:00"] != 2 {
				t.Fatalf("transient window must be retried once and succeed: %s", out)
			}
		})
	}
}

func TestPrivateSlicePersistentFailureReturnsNoData(t *testing.T) {
	for _, mode := range []string{"transport", "drift"} {
		t.Run(mode, func(t *testing.T) {
			attempts := sliceFixture(t, func(q portal.CallQuery, attempt int) (string, error) {
				if q.From != "2026-11-17 11:00:00" {
					return page(1, 4242), nil
				}
				if mode == "transport" {
					return "", fmt.Errorf("synthetic timeout")
				}
				if q.Offset == 1 {
					return page(3, 9), nil
				}
				return page(2, 7), nil
			})
			code, env, out := runSlices(t, append(append([]string{}, sliceSearch...), "--limit", "1")...)
			want := map[string]int{"transport": 6, "drift": 7}[mode]
			if code != want || strings.Contains(out, "4242") || env.Data.Rows != nil {
				t.Fatalf("want exit %d without partial data: %s", want, out)
			}
			d := env.Error.Details
			failed, _ := d["failed_slice"].(map[string]interface{})
			if failed["from"] != "2026-11-17 11:00:00" || failed["till"] != "2026-11-17 11:59:59" || failed["attempts"] != float64(2) || d["windows_completed"] != float64(1) || d["windows"] != float64(3) {
				t.Fatalf("failure must name the window and progress: %s", out)
			}
			if mode == "transport" && d["stage"] != "cabinet_transport" || mode == "drift" && d["reason"] != "population_changed" {
				t.Fatalf("original error shape lost: %s", out)
			}
			if attempts["2026-11-17 11:00:00"] != 2 || attempts["2026-11-17 12:00:00"] != 0 {
				t.Fatalf("one retry, then stop: %v", attempts)
			}
		})
	}
}

func TestPrivateSliceRefreshesSessionOnce(t *testing.T) {
	f := newRefreshFixture(t)
	r := f.run("calls", "search", "--source", "private", "--all", "--slice", "1h", "--limit", "10", "--from", "2026-11-16 00:00:00", "--till", "2026-11-16 02:59:59")
	if r.code != 0 || !strings.Contains(r.out, `"windows":3`) {
		t.Fatalf("exit=%d %s", r.code, r.out)
	}
	// Stale first read, then one fresh read per window on the refreshed session.
	if f.logins != 1 || f.reports != 4 {
		t.Fatalf("logins=%d reports=%d, want one refresh carried across windows", f.logins, f.reports)
	}
}
