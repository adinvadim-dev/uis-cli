package spec

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed data/api_spec.json
var apiSpecJSON []byte

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
	APIVersion  string  `json:"api_version,omitempty"`
	Description string  `json:"description,omitempty"`
	Params      []Param `json:"params,omitempty"`
}

func Load() ([]Method, error) {
	var ms []Method
	if len(apiSpecJSON) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(apiSpecJSON, &ms); err != nil {
		return nil, err
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Method < ms[j].Method })
	return ms, nil
}

func SplitMethod(m string) []string {
	parts := strings.Split(m, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
