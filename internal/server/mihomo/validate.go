package mihomo

import (
	"fmt"
	"strings"
)

type DiagnosticSeverity string

const (
	DiagnosticError   DiagnosticSeverity = "error"
	DiagnosticWarning DiagnosticSeverity = "warning"
)

type Diagnostic struct {
	Severity DiagnosticSeverity `json:"severity"`
	Code     string             `json:"code"`
	Path     string             `json:"path"`
	Message  string             `json:"message"`
}

func Validate(raw []byte) []Diagnostic {
	profile, err := decodeProfile(raw)
	if err != nil {
		return []Diagnostic{{
			Severity: DiagnosticError,
			Code:     "invalid_yaml",
			Path:     "$",
			Message:  err.Error(),
		}}
	}

	diagnostics := make([]Diagnostic, 0)
	known := map[string]string{
		"DIRECT": "built-in", "REJECT": "built-in", "REJECT-DROP": "built-in",
		"PASS": "built-in", "COMPATIBLE": "built-in",
	}

	proxies, ok := profile["proxies"].([]any)
	if !ok {
		diagnostics = append(diagnostics, errorDiagnostic("invalid_proxies", "proxies", "proxies must be an array"))
	} else {
		for index, item := range proxies {
			proxy, ok := item.(map[string]any)
			if !ok {
				diagnostics = append(diagnostics, errorDiagnostic("invalid_proxy", fmt.Sprintf("proxies[%d]", index), "proxy must be an object"))
				continue
			}
			name, _ := proxy["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				diagnostics = append(diagnostics, errorDiagnostic("missing_name", fmt.Sprintf("proxies[%d].name", index), "proxy name is required"))
				continue
			}
			if previous, exists := known[name]; exists {
				diagnostics = append(diagnostics, errorDiagnostic("duplicate_name", fmt.Sprintf("proxies[%d].name", index), fmt.Sprintf("proxy name %q conflicts with %s", name, previous)))
				continue
			}
			known[name] = fmt.Sprintf("proxies[%d]", index)
		}
	}

	groups, ok := profile["proxy-groups"].([]any)
	groupMembers := make(map[string][]string)
	if !ok {
		diagnostics = append(diagnostics, errorDiagnostic("invalid_proxy_groups", "proxy-groups", "proxy-groups must be an array"))
	} else {
		for index, item := range groups {
			group, ok := item.(map[string]any)
			if !ok {
				diagnostics = append(diagnostics, errorDiagnostic("invalid_proxy_group", fmt.Sprintf("proxy-groups[%d]", index), "proxy group must be an object"))
				continue
			}
			name, _ := group["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				diagnostics = append(diagnostics, errorDiagnostic("missing_name", fmt.Sprintf("proxy-groups[%d].name", index), "proxy group name is required"))
				continue
			}
			if previous, exists := known[name]; exists {
				diagnostics = append(diagnostics, errorDiagnostic("duplicate_name", fmt.Sprintf("proxy-groups[%d].name", index), fmt.Sprintf("proxy group name %q conflicts with %s", name, previous)))
			} else {
				known[name] = fmt.Sprintf("proxy-groups[%d]", index)
			}
			members, _ := group["proxies"].([]any)
			for memberIndex, rawMember := range members {
				member, ok := rawMember.(string)
				if !ok || strings.TrimSpace(member) == "" {
					diagnostics = append(diagnostics, errorDiagnostic("invalid_reference", fmt.Sprintf("proxy-groups[%d].proxies[%d]", index, memberIndex), "proxy group member must be a name"))
					continue
				}
				groupMembers[name] = append(groupMembers[name], member)
			}
		}
	}

	for group, members := range groupMembers {
		for index, member := range members {
			if _, exists := known[member]; !exists {
				diagnostics = append(diagnostics, errorDiagnostic("unknown_reference", fmt.Sprintf("proxy-groups[%s].proxies[%d]", group, index), fmt.Sprintf("unknown proxy or group %q", member)))
			}
		}
	}
	for _, group := range findGroupCycles(groupMembers) {
		diagnostics = append(diagnostics, errorDiagnostic("group_cycle", "proxy-groups", fmt.Sprintf("proxy group cycle includes %q", group)))
	}

	rules, ok := profile["rules"].([]any)
	if !ok || len(rules) == 0 {
		diagnostics = append(diagnostics, errorDiagnostic("invalid_rules", "rules", "rules must be a non-empty array"))
	} else {
		last, ok := rules[len(rules)-1].(string)
		if !ok || !strings.EqualFold(strings.TrimSpace(strings.SplitN(last, ",", 2)[0]), "MATCH") {
			diagnostics = append(diagnostics, errorDiagnostic("terminal_match", fmt.Sprintf("rules[%d]", len(rules)-1), "the final rule must be MATCH,<policy>"))
		}
	}
	return diagnostics
}

func HasErrors(diagnostics []Diagnostic) bool {
	return hasErrorDiagnostics(diagnostics)
}

func hasErrorDiagnostics(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == DiagnosticError {
			return true
		}
	}
	return false
}

func errorDiagnostic(code, path, message string) Diagnostic {
	return Diagnostic{Severity: DiagnosticError, Code: code, Path: path, Message: message}
}

func findGroupCycles(groups map[string][]string) []string {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(groups))
	cycles := make(map[string]struct{})
	var visit func(string)
	visit = func(group string) {
		switch state[group] {
		case visiting:
			cycles[group] = struct{}{}
			return
		case visited:
			return
		}
		state[group] = visiting
		for _, member := range groups[group] {
			if _, isGroup := groups[member]; isGroup {
				visit(member)
			}
		}
		state[group] = visited
	}
	for group := range groups {
		visit(group)
	}
	out := make([]string, 0, len(cycles))
	for group := range cycles {
		out = append(out, group)
	}
	return out
}
