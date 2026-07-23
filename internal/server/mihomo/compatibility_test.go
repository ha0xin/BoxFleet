package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type compatibilityFixture struct {
	SubStoreCommit string              `json:"substore_commit"`
	Cases          []compatibilityCase `json:"cases"`
}

type compatibilityCase struct {
	ID             string          `json:"id"`
	Classification string          `json:"classification"`
	Difference     string          `json:"difference"`
	Base           string          `json:"base"`
	Rewrites       []Rewrite       `json:"rewrites"`
	BoxFleetError  ErrorKind       `json:"boxfleet_error"`
	SubStore       subStoreOutcome `json:"substore"`
}

type subStoreOutcome struct {
	OK    bool   `json:"ok"`
	YAML  string `json:"yaml"`
	Error string `json:"error"`
}

func TestRecordedSubStoreCompatibility(t *testing.T) {
	fixture := loadCompatibilityFixture(t)
	compiler := NewCompiler(DefaultLimits())

	for _, tc := range fixture.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			result, err := compiler.Compile(context.Background(), []byte(tc.Base), tc.Rewrites)
			switch tc.Classification {
			case "compatible":
				if err != nil {
					t.Fatal(err)
				}
				if !tc.SubStore.OK {
					t.Fatal("compatible case records a Sub-Store failure")
				}
				assertYAMLEqual(t, result.YAML, []byte(tc.SubStore.YAML))
			case "intentional_difference":
				if strings.TrimSpace(tc.Difference) == "" {
					t.Fatal("intentional difference must explain why behavior differs")
				}
				var compileErr *CompileError
				if !errors.As(err, &compileErr) || compileErr.Kind != tc.BoxFleetError {
					t.Fatalf("BoxFleet error = %#v, want %q", err, tc.BoxFleetError)
				}
			default:
				t.Fatalf("unknown compatibility classification %q", tc.Classification)
			}
		})
	}
}

func TestLiveSubStoreCompatibility(t *testing.T) {
	if os.Getenv("BOXFLEET_SUBSTORE_COMPAT") != "1" {
		t.Skip("set BOXFLEET_SUBSTORE_COMPAT=1 to run against refs/sub-store")
	}
	fixture := loadCompatibilityFixture(t)
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	subStoreRoot := filepath.Join(repoRoot, "refs", "sub-store")
	runner := filepath.Join(repoRoot, "scripts", "compat", "substore-mihomo-runner.cjs")

	commitOutput, err := exec.Command("git", "-C", subStoreRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("read Sub-Store commit: %v", err)
	}
	if commit := strings.TrimSpace(string(commitOutput)); commit != fixture.SubStoreCommit {
		t.Fatalf("Sub-Store checkout is %s, fixture records %s; refresh and review the matrix", commit, fixture.SubStoreCommit)
	}

	for _, tc := range fixture.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			input, err := json.Marshal(map[string]any{
				"base":     tc.Base,
				"rewrites": tc.Rewrites,
			})
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("node", runner)
			cmd.Dir = repoRoot
			cmd.Stdin = bytes.NewReader(input)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("run Sub-Store: %v\n%s", err, output)
			}
			var live subStoreOutcome
			if err := json.Unmarshal(output, &live); err != nil {
				t.Fatalf("decode Sub-Store output: %v\n%s", err, output)
			}
			if live.OK != tc.SubStore.OK {
				t.Fatalf("Sub-Store ok = %v, recorded %v; live error: %s", live.OK, tc.SubStore.OK, live.Error)
			}
			if live.OK {
				assertYAMLEqual(t, []byte(live.YAML), []byte(tc.SubStore.YAML))
			} else if live.Error != tc.SubStore.Error {
				t.Fatalf("Sub-Store error = %q, recorded %q", live.Error, tc.SubStore.Error)
			}
		})
	}
}

func loadCompatibilityFixture(t *testing.T) compatibilityFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "substore_compatibility.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture compatibilityFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SubStoreCommit == "" || len(fixture.Cases) == 0 {
		t.Fatal("compatibility fixture must identify Sub-Store and contain cases")
	}
	return fixture
}

func assertYAMLEqual(t *testing.T, gotRaw, wantRaw []byte) {
	t.Helper()
	var got any
	if err := yaml.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("decode got YAML: %v\n%s", err, gotRaw)
	}
	var want any
	if err := yaml.Unmarshal(wantRaw, &want); err != nil {
		t.Fatalf("decode want YAML: %v\n%s", err, wantRaw)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("YAML differs\n got: %#v\nwant: %#v\ngot YAML:\n%s\nwant YAML:\n%s", got, want, gotRaw, wantRaw)
	}
}
