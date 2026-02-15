package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type GenParam struct {
	Name        string
	Flag        string
	FlagJSON    string
	Required    bool
	Type        string
	Description string
}

type GenMethod struct {
	Method      string
	Command     string // subcommand name under the resource
	Title       string
	URL         string
	Description string
	Params      []GenParam
}

type GenResource struct {
	Command string // resource command name at top-level (kebab-case)
	Title   string
	Methods []GenMethod
}

// generatedResources is produced by tools/uis-gen into internal/cli/generated_resources.go.
var generatedResources []GenResource

func addGeneratedResourceCommands(root *cobra.Command, g *globalFlags) {
	for _, r := range generatedResources {
		r := r
		rc := &cobra.Command{
			Use:   r.Command,
			Short: r.Title,
			Long:  r.Title,
		}
		for _, m := range r.Methods {
			m := m
			mc := newGeneratedMethodCmd(g, r, m)
			rc.AddCommand(mc)
		}
		root.AddCommand(rc)
	}
}

func appendResourcesToHelp(cmd *cobra.Command) error {
	if len(generatedResources) == 0 {
		return nil
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "\nAll UIS methods (grouped into human-friendly commands):")
	_, _ = fmt.Fprintln(out, "  Tip: run `uis <resource> <action> --help` to see parameters and docs link.")

	// Stable ordering.
	rs := make([]GenResource, 0, len(generatedResources))
	rs = append(rs, generatedResources...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Command < rs[j].Command })

	for _, r := range rs {
		_, _ = fmt.Fprintf(out, "\n%s:\n", r.Command)
		// Methods are already sorted by command in generator, but keep it stable anyway.
		methods := make([]GenMethod, 0, len(r.Methods))
		methods = append(methods, r.Methods...)
		sort.Slice(methods, func(i, j int) bool { return methods[i].Command < methods[j].Command })
		for _, m := range methods {
			_, _ = fmt.Fprintf(out, "  - %s -> %s\n", m.Command, m.Method)
		}
	}
	return nil
}

func newGeneratedMethodCmd(g *globalFlags, r GenResource, m GenMethod) *cobra.Command {
	var paramsJSON string
	var paramsFile string
	var paramKVs []string

	c := &cobra.Command{
		Use:     m.Command,
		Short:   m.TitleOrMethod(),
		Long:    generatedMethodLongHelp(r, m),
		Example: generatedMethodExamples(r, m),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}
			if cfg.AccessToken == "" && !g.DryRun {
				return exitf(10, "Missing access token. Set UIS_TOKEN env var or run `uis auth login`.")
			}

			params, err := parseParams(paramKVs, paramsJSON, paramsFile)
			if err != nil {
				return err
			}

			// Apply per-param flags.
			for _, p := range m.Params {
				if p.FlagJSON != "" && cmd.Flags().Changed(p.FlagJSON) {
					s, _ := cmd.Flags().GetString(p.FlagJSON)
					var v any
					if err := json.Unmarshal([]byte(s), &v); err != nil {
						return exitf(2, "Invalid JSON for --%s: %v", p.FlagJSON, err)
					}
					params[p.Name] = v
					continue
				}
				if p.Flag != "" && cmd.Flags().Changed(p.Flag) {
					s, _ := cmd.Flags().GetString(p.Flag)
					v, err := parseHumanFlagValue(p, s)
					if err != nil {
						return err
					}
					params[p.Name] = v
				}
			}

			// Inject token unless explicitly provided.
			if cfg.AccessToken != "" {
				if _, ok := params["access_token"]; !ok {
					params["access_token"] = cfg.AccessToken
				}
			}

			for _, p := range m.Params {
				if !p.Required {
					continue
				}
				if p.Name == "access_token" {
					continue
				}
				if _, ok := params[p.Name]; !ok {
					return exitf(2, "Missing required param %q (see: uis %s %s --help)", p.Name, r.Command, m.Command)
				}
			}

			if g.DryRun {
				return writeDryRunRequest(g, m.Method, params)
			}

			cl, err := newClient(cfg)
			if err != nil {
				return err
			}
			ctx, cancel, err := ctxWithTimeout(cfg)
			if err != nil {
				return err
			}
			defer cancel()

			resp, httpStatus, raw, err := cl.Call(ctx, m.Method, params)
			if err != nil {
				if httpStatus == 401 || httpStatus == 403 {
					return exitf(10, "HTTP %d calling %s: %v", httpStatus, m.Method, err)
				}
				if httpStatus == 429 {
					return exitf(11, "HTTP 429 calling %s: %v", m.Method, err)
				}
				return exitf(12, "HTTP error calling %s: %v", m.Method, err)
			}
			if resp.Error != nil {
				return exitf(12, "%s: %s", m.Method, resp.Error.String())
			}

			if len(resp.Result) > 0 {
				return writeResult(g, resp.Result)
			}

			_, _ = os.Stdout.Write(append(raw, '\n'))
			return nil
		},
	}

	// Advanced/escape-hatch params injection.
	c.Flags().StringArrayVar(&paramKVs, "param", nil, "Advanced: param key=value or key:=<json> (repeatable)")
	c.Flags().StringVar(&paramsJSON, "params-json", "", "Advanced: params as a JSON object string")
	c.Flags().StringVar(&paramsFile, "params-file", "", "Advanced: read params JSON object from file ('-' for stdin)")

	// Per-param flags.
	for _, p := range m.Params {
		if p.Name == "access_token" {
			// injected by config/env; still allow override via advanced flags.
			continue
		}
		if p.Flag == "" || p.FlagJSON == "" {
			continue
		}

		help := strings.TrimSpace(p.Description)
		if help == "" {
			help = "param"
		}
		if p.Required {
			help += " (required)"
		}
		if p.Type != "" {
			help += " type=" + p.Type
		}

		c.Flags().String(p.Flag, "", help)
		c.Flags().String(p.FlagJSON, "", help+" (JSON)")
	}

	return c
}

func (m GenMethod) TitleOrMethod() string {
	if strings.TrimSpace(m.Title) != "" {
		return m.Title
	}
	return m.Method
}

func generatedMethodLongHelp(r GenResource, m GenMethod) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Resource: %s\n", r.Command))
	b.WriteString(fmt.Sprintf("Method:   %s\n", m.Method))
	if m.URL != "" {
		b.WriteString(fmt.Sprintf("Docs:     %s\n", m.URL))
	}
	if m.Description != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(m.Description))
		b.WriteString("\n")
	}
	if len(m.Params) > 0 {
		b.WriteString("\nParams:\n")
		for _, p := range m.Params {
			req := "optional"
			if p.Required {
				req = "required"
			}
			line := fmt.Sprintf("  - %s (%s)", p.Name, req)
			if p.Type != "" {
				line += " type=" + p.Type
			}
			if p.Description != "" {
				line += " : " + strings.TrimSpace(p.Description)
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\nNotes:\n")
	b.WriteString("  - access_token is injected from config/env unless you override it via --param/--params-json.\n")
	b.WriteString("  - For non-string params, prefer --<name>-json.\n")
	return strings.TrimSpace(b.String())
}

func generatedMethodExamples(r GenResource, m GenMethod) string {
	var jsonParam string
	for _, p := range m.Params {
		if p.Name == "access_token" {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(p.Type))
		if t == "object" || t == "array" {
			jsonParam = "--" + flagify(p.Name) + "-json"
			break
		}
	}
	if jsonParam == "" {
		jsonParam = "--param limit:=10"
	}
	return strings.TrimSpace(fmt.Sprintf(`
  # %s
  uis %s %s --help

  # example invocation (adjust params as needed)
  uis %s %s %s
`, m.Method, r.Command, m.Command, r.Command, m.Command, jsonParam))
}
