package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"uis-cli/internal/portal"
)

func staffPayload(file string) (portal.Object, error) {
	if file == "" {
		return nil, exitf(2, "--payload-file is required")
	}
	f, e := os.Open(file)
	if e != nil {
		return nil, exitf(2, "Cannot read payload file")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(b) > 1<<20 {
		return nil, exitf(2, "Payload must be at most 1 MiB")
	}
	var patch portal.Object
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&patch) != nil || patch == nil || d.Decode(new(interface{})) != io.EOF {
		return nil, exitf(2, "Payload must be exactly one JSON object")
	}
	return patch, nil
}

func newStaff(g *globalFlags) *cobra.Command {
	root := &cobra.Command{Use: "staff", Short: "Employee cards, groups, softphone and SIP registration (private cabinet)"}
	for _, resource := range []string{"employees", "groups", "softphone", "sip-lines"} {
		resource := resource
		group := &cobra.Command{Use: resource}
		actions := []string{"get"}
		if resource != "softphone" {
			actions = append(actions, "list")
		}
		if resource != "groups" {
			actions = append(actions, "update")
		}
		if resource == "employees" {
			actions = append(actions, "groups")
		}
		for _, action := range actions {
			action := action
			use := action
			check := cobra.NoArgs
			if action != "list" {
				use += " <id>"
				check = cobra.ExactArgs(1)
			}
			cmd := &cobra.Command{Use: use, Args: check, RunE: func(cmd *cobra.Command, args []string) error {
				id := ""
				if len(args) > 0 {
					id = args[0]
				}
				patch := portal.Object{}
				if action == "update" {
					file, _ := cmd.Flags().GetString("payload-file")
					var e error
					patch, e = staffPayload(file)
					if e != nil {
						return e
					}
				}
				plan, e := portal.StaffPlan(resource, action, id, patch)
				if e != nil {
					return operationError(e)
				}
				employeeID, _ := cmd.Flags().GetString("employee-id")
				if employeeID != "" {
					n, e := strconv.ParseInt(employeeID, 10, 64)
					if e != nil || n <= 0 || strconv.FormatInt(n, 10) != employeeID {
						return exitf(2, "Employee ID must be a canonical positive integer")
					}
					plan["employee_id"] = employeeID
				}
				if g.DryRun {
					return emit(g, plan)
				}
				if action == "update" {
					if e := confirm(g, plan); e != nil {
						return e
					}
				}
				c, e := privateClient(g)
				if e != nil {
					return e
				}
				var v portal.Object
				switch action {
				case "list":
					v, e = c.StaffList(cmd.Context(), resource, employeeID)
				case "update":
					v, e = c.StaffUpdate(cmd.Context(), resource, id, patch)
				case "groups":
					v, e = c.StaffGroups(cmd.Context(), id)
				case "get":
					if resource == "softphone" {
						v, e = c.StaffSoftphone(cmd.Context(), id)
					} else {
						v, e = c.StaffGet(cmd.Context(), resource, id)
					}
				}
				if e != nil {
					return operationError(e)
				}
				return emit(g, v)
			}}
			if action == "update" {
				cmd.Flags().String("payload-file", "", "JSON patch with supported fields only; values never echoed")
			}
			if resource == "sip-lines" && action == "list" {
				cmd.Flags().String("employee-id", "", "Filter by exact employee ID locally")
			}
			group.AddCommand(cmd)
		}
		root.AddCommand(group)
	}
	return root
}
