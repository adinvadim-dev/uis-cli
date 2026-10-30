package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"uis-cli/internal/archive"
	"uis-cli/internal/portal"
)

func newAutodial(g *globalFlags) *cobra.Command {
	root := &cobra.Command{Use: "autodial", Short: "Outgoing campaign reads and guarded mutation plans (private cabinet)"}
	for _, action := range []string{"list", "get", "statuses", "calls", "subscribers", "employees", "stop-list", "validate", "export", "create", "update", "status", "exclude", "include", "import", "delete", "listen"} {
		action := action
		needsID := action == "get" || action == "calls" || action == "subscribers" || action == "update" || action == "status" || action == "delete"
		use := action
		argsCheck := cobra.NoArgs
		if needsID {
			use += " <campaign-id>"
			argsCheck = cobra.ExactArgs(1)
		}
		cmd := &cobra.Command{Use: use, Args: argsCheck, RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if needsID {
				id = args[0]
				if _, e := positive(id); e != nil {
					return e
				}
			}
			file, _ := cmd.Flags().GetString("payload-file")
			payload := portal.Object{}
			if file != "" {
				f, e := os.Open(file)
				if e != nil {
					return exitf(2, "Cannot read payload file")
				}
				defer f.Close()
				b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
				if e != nil || len(b) > 1<<20 {
					return exitf(2, "Payload must be at most 1 MiB")
				}
				d := json.NewDecoder(bytes.NewReader(b))
				d.UseNumber()
				if d.Decode(&payload) != nil || payload == nil || d.Decode(new(interface{})) != io.EOF {
					return exitf(2, "Payload must be exactly one JSON object")
				}
			}
			switch action {
			case "list", "export", "validate", "create", "update", "status", "exclude", "include", "listen":
				if file == "" {
					return exitf(2, "--payload-file is required; see docs/research/06-autodial-coverage.md")
				}
			}
			if action == "import" {
				input, _ := cmd.Flags().GetString("input-file")
				if input == "" {
					return exitf(2, "--input-file is required")
				}
				payload["input_file"] = input
			}
			if action == "list" {
				from, ok := payload["begin_date"].(string)
				till, ok2 := payload["end_date"].(string)
				if !ok || !ok2 {
					return exitf(2, "List payload requires begin_date and end_date")
				}
				f, e := time.Parse("2006-01-02 15:04:05", from)
				t, e2 := time.Parse("2006-01-02 15:04:05", till)
				if e != nil || e2 != nil || t.Before(f) {
					return exitf(2, "Invalid campaign date window")
				}
			}
			r, e := portal.AutodialPlan(action, id, payload)
			if e != nil {
				return operationError(e)
			}
			fields := make([]string, 0, len(payload))
			for k := range payload {
				fields = append(fields, k)
			}
			sort.Strings(fields)
			// Plans deliberately omit values: phone lists, TTS and comments may contain
			// private information. No credential/session I/O occurs on this path.
			plan := portal.Object{"source": "private.supervisor", "method": r.Method, "path": r.Path, "campaign_id": id, "payload_fields": fields, "mutation": r.Write, "network": false, "live_enabled": !r.Write || action == "exclude" || action == "include"}
			if g.DryRun {
				return emit(g, plan)
			}
			if r.Write {
				if action != "exclude" && action != "include" {
					return exitf(7, "Autodial write contract unverified: use --dry-run; see coverage inventory")
				}
				if e := confirm(g, plan); e != nil {
					return e
				}
			}
			c, e := privateClient(g)
			if e != nil {
				return e
			}
			v, e := c.Autodial(cmd.Context(), action, id, payload)
			if e != nil {
				return operationError(e)
			}
			return emit(g, portal.Object{"source": "private.supervisor", "data": archive.Scrub(v), "limitations": []string{"Source-observed private contract; no snapshot or completeness guarantee"}})
		}}
		if action == "import" {
			cmd.Flags().String("input-file", "", "CSV/XLSX file for server-side parsing; does not save a campaign")
		}
		cmd.Flags().String("payload-file", "", "Local JSON object; values never echoed in plans")
		root.AddCommand(cmd)
	}
	return root
}
