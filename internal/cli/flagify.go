package cli

import "strings"

func flagify(name string) string {
	// Convert snake_case to kebab-case, strip unsafe characters.
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "_", "-")
	var out strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		} else {
			out.WriteRune('-')
		}
	}
	s := strings.Trim(out.String(), "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}
