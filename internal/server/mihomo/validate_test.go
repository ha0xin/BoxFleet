package mihomo

import "testing"

func TestValidateCompleteProfile(t *testing.T) {
	valid := []byte(`
proxies:
  - {name: HK-01, type: vless}
proxy-groups:
  - name: PROXY
    type: select
    proxies: [AUTO, DIRECT]
    include-all-proxies: true
  - name: AUTO
    type: url-test
    include-all-proxies: true
rules:
  - MATCH,PROXY
`)
	if diagnostics := Validate(valid); hasErrorDiagnostics(diagnostics) {
		t.Fatalf("valid profile diagnostics: %#v", diagnostics)
	}
}

func TestValidateReportsActionableProfileErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		code string
	}{
		{
			name: "duplicate proxy name",
			yaml: `
proxies:
  - {name: same, type: vless}
  - {name: same, type: vless}
proxy-groups: []
rules: ["MATCH,DIRECT"]
`,
			code: "duplicate_name",
		},
		{
			name: "unknown group member",
			yaml: `
proxies: []
proxy-groups:
  - {name: PROXY, type: select, proxies: [MISSING]}
rules: ["MATCH,PROXY"]
`,
			code: "unknown_reference",
		},
		{
			name: "group cycle",
			yaml: `
proxies: []
proxy-groups:
  - {name: A, type: select, proxies: [B]}
  - {name: B, type: select, proxies: [A]}
rules: ["MATCH,A"]
`,
			code: "group_cycle",
		},
		{
			name: "missing terminal match",
			yaml: `
proxies: []
proxy-groups:
  - {name: PROXY, type: select, include-all-proxies: true}
rules: ["GEOIP,CN,DIRECT"]
`,
			code: "terminal_match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := Validate([]byte(tt.yaml))
			if !containsDiagnosticCode(diagnostics, tt.code) {
				t.Fatalf("diagnostics = %#v, want code %q", diagnostics, tt.code)
			}
		})
	}
}

func containsDiagnosticCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
