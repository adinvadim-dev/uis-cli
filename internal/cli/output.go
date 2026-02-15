package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

func writeResult(g *globalFlags, raw json.RawMessage) error {
	// If result isn't valid JSON, just print it.
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		_, err2 := os.Stdout.Write(append([]byte(strings.TrimSpace(string(raw))), '\n'))
		return err2
	}

	if g.JSON {
		return writeJSON(g, v)
	}
	return writeHuman(v)
}

func writeHuman(v any) error {
	switch vv := v.(type) {
	case nil:
		_, err := fmt.Fprintln(os.Stdout, "<null>")
		return err
	case map[string]any:
		return writeHumanMap(vv)
	case []any:
		return writeList(vv)
	default:
		// string/number/bool
		_, err := fmt.Fprintln(os.Stdout, fmt.Sprint(vv))
		return err
	}
}

func writeHumanMap(m map[string]any) error {
	// Common UIS shape: { "data": [...], "metadata": {...} }
	dataAny, hasData := m["data"]
	data, dataIsList := dataAny.([]any)
	metaAny, hasMeta := m["metadata"]
	meta, metaIsMap := metaAny.(map[string]any)

	if hasData && dataIsList {
		if hasMeta && metaIsMap && len(meta) > 0 {
			_, _ = fmt.Fprintln(os.Stdout, "metadata:")
			if err := writeKVWithPrefix(meta, "  "); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout)
		}

		_, _ = fmt.Fprintf(os.Stdout, "data (%d):\n", len(data))
		if err := writeList(data); err != nil {
			return err
		}

		// Any other top-level keys.
		rest := map[string]any{}
		for k, v := range m {
			if k == "data" || k == "metadata" {
				continue
			}
			rest[k] = v
		}
		if len(rest) > 0 {
			_, _ = fmt.Fprintln(os.Stdout)
			if err := writeKV(rest); err != nil {
				return err
			}
		}
		return nil
	}

	return writeKV(m)
}

func writeKV(m map[string]any) error {
	return writeKVWithPrefix(m, "")
}

func writeKVWithPrefix(m map[string]any, prefix string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "%s%s:\t%s\n", prefix, k, humanValue(m[k]))
	}
	return w.Flush()
}

func writeList(xs []any) error {
	// If it's a list of objects, render a table. Otherwise, one item per line.
	objs := make([]map[string]any, 0, len(xs))
	for _, x := range xs {
		o, ok := x.(map[string]any)
		if !ok {
			objs = nil
			break
		}
		objs = append(objs, o)
	}
	if objs == nil {
		for _, x := range xs {
			_, _ = fmt.Fprintln(os.Stdout, humanValue(x))
		}
		return nil
	}
	return writeTable(objs)
}

func writeTable(rows []map[string]any) error {
	// Columns: prioritize common identifiers.
	wantFirst := []string{
		"id",
		"phone_number",
		"location_name",
		"category",
		"name",
		"title",
		"status",
		"monthly_charge",
		"onetime_payment",
		"created_at",
		"creation_time",
		"date",
		"time",
	}

	colSet := map[string]bool{}
	// Sample first N rows to keep columns bounded.
	N := len(rows)
	if N > 50 {
		N = 50
	}
	for i := 0; i < N; i++ {
		for k := range rows[i] {
			colSet[k] = true
		}
	}

	cols := make([]string, 0, len(colSet))
	for _, k := range wantFirst {
		if colSet[k] {
			cols = append(cols, k)
			delete(colSet, k)
		}
	}
	rest := make([]string, 0, len(colSet))
	for k := range colSet {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	cols = append(cols, rest...)

	// Keep tables readable: cap the number of columns.
	if len(cols) > 12 {
		cols = cols[:12]
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, strings.Join(cols, "\t"))
	for _, r := range rows {
		cells := make([]string, 0, len(cols))
		for _, c := range cols {
			cells = append(cells, humanCell(r[c]))
		}
		_, _ = fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	return w.Flush()
}

func humanCell(v any) string {
	s := humanValue(v)
	// Keep row width reasonable.
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	// Avoid newlines in tables.
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func humanValue(v any) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case string:
		return vv
	case bool:
		if vv {
			return "true"
		}
		return "false"
	case float64:
		// JSON numbers come as float64; print compact.
		return fmt.Sprintf("%v", vv)
	case map[string]any, []any:
		// Keep it one-line in human output; multi-line breaks tables/KV layouts.
		b, err := json.Marshal(vv)
		if err != nil {
			return fmt.Sprint(vv)
		}
		return string(b)
	default:
		return fmt.Sprint(vv)
	}
}
