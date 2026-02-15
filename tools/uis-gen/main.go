package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"
)

type Param struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

type Method struct {
	Method      string  `json:"method"`
	Title       string  `json:"title,omitempty"`
	URL         string  `json:"url,omitempty"`
	Description string  `json:"description,omitempty"`
	Params      []Param `json:"params,omitempty"`
}

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
	Command     string
	Title       string
	URL         string
	Description string
	Params      []GenParam
}

type GenResource struct {
	Command string
	Title   string
	Methods []GenMethod
}

func main() {
	var inPath string
	var outPath string
	flag.StringVar(&inPath, "in", "internal/spec/data/api_spec.json", "Input JSON spec path")
	flag.StringVar(&outPath, "out", "internal/cli/generated_resources.go", "Output Go file path")
	flag.Parse()

	b, err := os.ReadFile(inPath)
	if err != nil {
		fatal("read %s: %v", inPath, err)
	}

	var ms []Method
	if err := json.Unmarshal(b, &ms); err != nil {
		fatal("parse %s: %v", inPath, err)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Method < ms[j].Method })

	res := buildResources(ms)
	out, err := render(outPath, res)
	if err != nil {
		fatal("render: %v", err)
	}
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		fatal("write %s: %v", outPath, err)
	}
	_, _ = fmt.Fprintf(os.Stderr, "wrote %d resources to %s\n", len(res), outPath)
}

func buildResources(ms []Method) []GenResource {
	type verbsSet map[string]bool
	byRest := map[string]verbsSet{}
	for _, m := range ms {
		verb, rest, ok := splitMethod(m.Method)
		if !ok {
			continue
		}
		s := byRest[rest]
		if s == nil {
			s = verbsSet{}
			byRest[rest] = s
		}
		s[verb] = true
	}

	coreRest := map[string]bool{}
	for rest, vs := range byRest {
		count := 0
		for v := range vs {
			switch v {
			case "get", "create", "update", "delete", "enable", "disable":
				count++
			}
		}
		if count >= 3 {
			coreRest[rest] = true
		}
	}

	// plural -> singular mapping: calls -> call, campaigns -> campaign, employees -> employee, etc.
	singularToPlural := map[string]string{}
	for rest := range byRest {
		if strings.HasSuffix(rest, "s") && len(rest) > 1 {
			s := strings.TrimSuffix(rest, "s")
			if s != "" {
				singularToPlural[s] = rest
			}
		}
	}

	groups := map[string][]GenMethod{}

	for _, m := range ms {
		verb, rest, ok := splitMethod(m.Method)
		if !ok {
			continue
		}

		group := rest
		mappedPlural := ""
		if !coreRest[rest] {
			// Map campaign_* -> campaigns, call_* -> calls, etc.
			for s, p := range singularToPlural {
				if p == "" {
					continue
				}
				if rest == s || strings.HasPrefix(rest, s+"_") {
					if coreRest[p] {
						group = p
						mappedPlural = p
						break
					}
				}
			}
		}

		cmd := subcommandName(verb, rest, group, mappedPlural)

		usedFlags := map[string]bool{
			"help":        true,
			"param":       true,
			"params-json": true,
			"params-file": true,
		}
		params := make([]GenParam, 0, len(m.Params))
		for _, p := range m.Params {
			base := flagify(p.Name)
			if base == "" {
				base = "param"
			}
			flagName := base
			flagJSON := base + "-json"
			// Ensure per-method uniqueness (Cobra panics on redefinition).
			if usedFlags[flagName] || usedFlags[flagJSON] {
				for i := 2; i < 1000; i++ {
					try := fmt.Sprintf("%s-%d", base, i)
					tryJSON := try + "-json"
					if usedFlags[try] || usedFlags[tryJSON] {
						continue
					}
					flagName = try
					flagJSON = tryJSON
					break
				}
			}
			usedFlags[flagName] = true
			usedFlags[flagJSON] = true

			params = append(params, GenParam{
				Name:        p.Name,
				Flag:        flagName,
				FlagJSON:    flagJSON,
				Required:    p.Required,
				Type:        p.Type,
				Description: compactSpace(p.Description),
			})
		}

		groups[group] = append(groups[group], GenMethod{
			Method:      m.Method,
			Command:     cmd,
			Title:       compactSpace(m.Title),
			URL:         m.URL,
			Description: compactSpace(m.Description),
			Params:      params,
		})
	}

	// Resolve collisions per resource: if two methods map to same Command, prefix with verb or fallback to method tail.
	resources := make([]GenResource, 0, len(groups))
	for group, methods := range groups {
		seen := map[string]int{}
		for i := range methods {
			seen[methods[i].Command]++
		}
		for i := range methods {
			if seen[methods[i].Command] <= 1 {
				continue
			}
			verb, rest, _ := splitMethod(methods[i].Method)
			fixed := verb + "-" + methods[i].Command
			if fixed == methods[i].Command || fixed == "" {
				fixed = verb + "-" + kebab(rest)
			}
			methods[i].Command = fixed
		}

		sort.Slice(methods, func(i, j int) bool { return methods[i].Command < methods[j].Command })
		resources = append(resources, GenResource{
			Command: kebab(group),
			Title:   humanTitle(group),
			Methods: methods,
		})
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].Command < resources[j].Command })
	return resources
}

func subcommandName(verb, rest, group, mappedPlural string) string {
	// Core resource: get.campaigns -> list, create.campaigns -> create, ...
	if rest == group {
		switch verb {
		case "get":
			if strings.HasSuffix(group, "s") {
				return "list"
			}
			return "get"
		default:
			return verb
		}
	}

	// Mapped under a plural core resource (campaign_* under campaigns):
	// prefer tail without the singular prefix when possible.
	tail := rest
	if mappedPlural != "" && strings.HasSuffix(mappedPlural, "s") {
		sing := strings.TrimSuffix(mappedPlural, "s")
		if rest == sing {
			tail = ""
		} else if strings.HasPrefix(rest, sing+"_") {
			tail = strings.TrimPrefix(rest, sing+"_")
		}
	}
	if tail == "" {
		tail = rest
	}
	cmd := kebab(tail)
	if verb == "get" {
		return cmd
	}
	return verb + "-" + cmd
}

func splitMethod(m string) (verb string, rest string, ok bool) {
	parts := strings.SplitN(m, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func kebab(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ToLower(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

func flagify(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "_", "-")
	var out strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		} else {
			out.WriteRune('-')
		}
	}
	res := strings.Trim(out.String(), "-")
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	return strings.ToLower(res)
}

func humanTitle(group string) string {
	// A simple title that remains stable even as docs change.
	return fmt.Sprintf("%s operations", strings.ReplaceAll(group, "_", " "))
}

func compactSpace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func render(outPath string, resources []GenResource) ([]byte, error) {
	const tpl = `// Code generated by tools/uis-gen; DO NOT EDIT.
package cli

func init() {
	generatedResources = []GenResource{
{{- range . }}
		{
			Command: {{ printf "%q" .Command }},
			Title:   {{ printf "%q" .Title }},
			Methods: []GenMethod{
{{- range .Methods }}
				{
					Method:      {{ printf "%q" .Method }},
					Command:     {{ printf "%q" .Command }},
					Title:       {{ printf "%q" .Title }},
					URL:         {{ printf "%q" .URL }},
					Description: {{ printf "%q" .Description }},
					Params: []GenParam{
{{- range .Params }}
						{
							Name:        {{ printf "%q" .Name }},
							Flag:        {{ printf "%q" .Flag }},
							FlagJSON:    {{ printf "%q" .FlagJSON }},
							Required:    {{ .Required }},
							Type:        {{ printf "%q" .Type }},
							Description: {{ printf "%q" .Description }},
						},
{{- end }}
					},
				},
{{- end }}
			},
		},
{{- end }}
	}
}
`
	t, err := template.New("gen").Parse(tpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, resources); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
