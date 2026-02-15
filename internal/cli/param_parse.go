package cli

import (
	"encoding/json"
	"strconv"
	"strings"
)

func parseHumanFlagValue(p GenParam, s string) (any, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(strings.TrimSpace(p.Type)) {
	case "number":
		// Prefer int when possible; fall back to float.
		if strings.ContainsAny(s, ".eE") {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, err
			}
			return f, nil
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, err
		}
		return b, nil
	case "array":
		// Allow JSON array in the plain flag for convenience.
		if strings.HasPrefix(s, "[") {
			var v any
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				return nil, err
			}
			return v, nil
		}
		// Special-case the common "fields" param: accept CSV.
		if p.Name == "fields" {
			parts := strings.Split(s, ",")
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				out = append(out, part)
			}
			return out, nil
		}
		return nil, errNeedsJSON(p)
	case "object":
		// Allow JSON object in the plain flag for convenience.
		if strings.HasPrefix(s, "{") {
			var v any
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				return nil, err
			}
			return v, nil
		}
		return nil, errNeedsJSON(p)
	default:
		return s, nil
	}
}

func errNeedsJSON(p GenParam) error {
	if p.FlagJSON != "" {
		return exitf(2, "Param %q expects %s; use --%s", p.Name, p.Type, p.FlagJSON)
	}
	return exitf(2, "Param %q expects %s; use the -json variant", p.Name, p.Type)
}
