package mihomo

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestCompileClashPartyYAMLMergeSemantics(t *testing.T) {
	base := []byte(`
mixed-port: 7890
dns:
  enable: true
  ipv6: true
  nameserver:
    - 1.1.1.1
rules:
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
proxy-groups:
  - name: PROXY
    type: select
    proxies: [DIRECT]
`)

	tests := []struct {
		name  string
		patch string
		want  map[string]any
	}{
		{
			name: "recursively merge objects and replace scalars and arrays",
			patch: `
mixed-port: 7891
dns:
  ipv6: false
  nameserver: [8.8.8.8]
`,
			want: map[string]any{
				"mixed-port": 7891,
				"dns": map[string]any{
					"enable":     true,
					"ipv6":       false,
					"nameserver": []any{"8.8.8.8"},
				},
				"rules": []any{"GEOIP,CN,DIRECT", "MATCH,PROXY"},
				"proxy-groups": []any{
					map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"DIRECT"}},
				},
			},
		},
		{
			name: "prepend and append arrays",
			patch: `
+rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
rules+:
  - PROCESS-NAME,curl,DIRECT
`,
			want: map[string]any{
				"mixed-port": 7890,
				"dns": map[string]any{
					"enable": true, "ipv6": true, "nameserver": []any{"1.1.1.1"},
				},
				"rules": []any{
					"DOMAIN-SUFFIX,example.com,DIRECT",
					"GEOIP,CN,DIRECT",
					"MATCH,PROXY",
					"PROCESS-NAME,curl,DIRECT",
				},
				"proxy-groups": []any{
					map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"DIRECT"}},
				},
			},
		},
		{
			name: "force replace an object",
			patch: `
dns!:
  enable: false
`,
			want: map[string]any{
				"mixed-port": 7890,
				"dns":        map[string]any{"enable": false},
				"rules":      []any{"GEOIP,CN,DIRECT", "MATCH,PROXY"},
				"proxy-groups": []any{
					map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"DIRECT"}},
				},
			},
		},
		{
			name: "unwrap an ambiguous literal key",
			patch: `
dns:
  nameserver-policy:
    <+.example.com>:
      - 8.8.4.4
`,
			want: map[string]any{
				"mixed-port": 7890,
				"dns": map[string]any{
					"enable":     true,
					"ipv6":       true,
					"nameserver": []any{"1.1.1.1"},
					"nameserver-policy": map[string]any{
						"+.example.com": []any{"8.8.4.4"},
					},
				},
				"rules": []any{"GEOIP,CN,DIRECT", "MATCH,PROXY"},
				"proxy-groups": []any{
					map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"DIRECT"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotYAML, err := MergeYAML(base, []byte(tt.patch))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := yaml.Unmarshal(gotYAML, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("merged profile mismatch\n got: %#v\nwant: %#v\nyaml:\n%s", got, tt.want, gotYAML)
			}
		})
	}
}

func TestCompileAppliesMixedRewritesInOrder(t *testing.T) {
	compiler := NewCompiler(DefaultLimits())
	result, err := compiler.Compile(context.Background(), []byte(`
proxies:
  - {name: HK-01, type: vless}
rules: [MATCH,DIRECT]
`), []Rewrite{
		{
			Name: "basic",
			Kind: RewriteYAML,
			Content: `
mode: rule
proxy-groups:
  - name: PROXY
    type: select
    include-all-proxies: true
`,
		},
		{
			Name: "rename",
			Kind: RewriteJavaScript,
			Content: `
function main(config) {
  config.proxies[0].name = "Hong Kong"
  console.log("renamed", config.proxies.length)
  return config
}
`,
		},
		{
			Name:    "rules",
			Kind:    RewriteYAML,
			Content: "+rules:\n  - DOMAIN-SUFFIX,example.com,PROXY\n",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := yaml.Unmarshal(result.YAML, &got); err != nil {
		t.Fatal(err)
	}
	proxies := got["proxies"].([]any)
	if proxies[0].(map[string]any)["name"] != "Hong Kong" {
		t.Fatalf("unexpected proxies: %#v", proxies)
	}
	rules := got["rules"].([]any)
	if !reflect.DeepEqual(rules, []any{"DOMAIN-SUFFIX,example.com,PROXY", "MATCH", "DIRECT"}) {
		t.Fatalf("unexpected rules: %#v", rules)
	}
	if len(result.Logs) != 1 || result.Logs[0].Rewrite != "rename" || result.Logs[0].Message != "renamed 1" {
		t.Fatalf("unexpected logs: %#v", result.Logs)
	}
}

func TestCompileRejectsInvalidJavaScriptResults(t *testing.T) {
	compiler := NewCompiler(DefaultLimits())
	tests := []struct {
		name   string
		script string
		kind   ErrorKind
	}{
		{name: "missing main", script: "const value = 1", kind: ErrorInvalidScript},
		{name: "non object", script: "function main(config) { return 42 }", kind: ErrorInvalidResult},
		{name: "promise", script: "async function main(config) { return config }", kind: ErrorAsyncUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compiler.Compile(context.Background(), []byte("proxies: []\n"), []Rewrite{{
				Name:    "bad",
				Kind:    RewriteJavaScript,
				Content: tt.script,
			}})
			var compileErr *CompileError
			if !errors.As(err, &compileErr) || compileErr.Kind != tt.kind || compileErr.Rewrite != "bad" {
				t.Fatalf("error = %#v, want kind %q for bad", err, tt.kind)
			}
		})
	}
}

func TestJavaScriptRuntimeDoesNotExposeHostCapabilities(t *testing.T) {
	compiler := NewCompiler(DefaultLimits())
	_, err := compiler.Compile(context.Background(), []byte("proxies: []\n"), []Rewrite{{
		Name: "sandbox",
		Kind: RewriteJavaScript,
		Content: `function main(config) {
  if (typeof require !== "undefined" || typeof process !== "undefined" ||
      typeof fetch !== "undefined" || typeof setTimeout !== "undefined") {
    throw new Error("host capability exposed")
  }
  return config
}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompileInterruptsJavaScript(t *testing.T) {
	limits := DefaultLimits()
	limits.ScriptTimeout = 20 * time.Millisecond
	compiler := NewCompiler(limits)
	started := time.Now()
	_, err := compiler.Compile(context.Background(), []byte("proxies: []\n"), []Rewrite{{
		Name:    "loop",
		Kind:    RewriteJavaScript,
		Content: "function main(config) { while (true) {} }",
	}})
	if time.Since(started) > time.Second {
		t.Fatalf("script timeout took too long: %s", time.Since(started))
	}
	var compileErr *CompileError
	if !errors.As(err, &compileErr) || compileErr.Kind != ErrorTimeout {
		t.Fatalf("error = %#v, want timeout", err)
	}
}

func TestCompileCapsConsoleOutput(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxLogBytes = 8
	compiler := NewCompiler(limits)
	result, err := compiler.Compile(context.Background(), []byte("proxies: []\n"), []Rewrite{{
		Name:    "logging",
		Kind:    RewriteJavaScript,
		Content: `function main(config) { console.log("123456789"); return config }`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != 1 || !strings.Contains(result.Logs[0].Message, "truncated") {
		t.Fatalf("unexpected logs: %#v", result.Logs)
	}
}

func TestCompileRejectsUnknownRewriteKind(t *testing.T) {
	compiler := NewCompiler(DefaultLimits())
	_, err := compiler.Compile(context.Background(), []byte("proxies: []\n"), []Rewrite{{
		Name: "unknown", Kind: RewriteKind("lua"), Content: "return {}",
	}})
	var compileErr *CompileError
	if !errors.As(err, &compileErr) || compileErr.Kind != ErrorInvalidRewrite {
		t.Fatalf("error = %#v, want invalid rewrite", err)
	}
}
