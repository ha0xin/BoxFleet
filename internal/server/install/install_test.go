package install

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerPassesBootstrapOnStdinAndCleansUp(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		t.Run(fmt.Sprintf("interrupt=%v", interrupt), func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "fixtures")
			mock := filepath.Join(dir, "mock")
			scratch := filepath.Join(dir, "scratch")
			for _, path := range []string{fixture, mock, scratch} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			agent := []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEST_ARGS\"\ncat > \"$TEST_BOOTSTRAP\"\n")
			sing := []byte("fake sing-box binary")
			assets := map[string][]byte{"boxfleet-agent-v0.8.2-linux-amd64": agent, "sing-box-v1.14.2-linux-amd64": sing}
			sums := ""
			for name, data := range assets {
				if err := os.WriteFile(filepath.Join(fixture, name), data, 0o755); err != nil {
					t.Fatal(err)
				}
				sums += fmt.Sprintf("%x  artifacts/%s\n", sha256.Sum256(data), name)
			}
			if err := os.WriteFile(filepath.Join(fixture, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
				t.Fatal(err)
			}
			curl := `#!/bin/sh
if [ "$TEST_INTERRUPT" = yes ]; then
 kill -TERM "$PPID"
 exit 0
fi
while [ "$#" -gt 0 ]; do
 case "$1" in
  -o) dest="$2"; shift 2 ;;
  --connect-timeout|--max-time|--retry) shift 2 ;;
  -*) shift ;;
  *) url="$1"; shift ;;
 esac
done
cp "$TEST_FIXTURE/${url##*/}" "$dest"
`
			if err := os.WriteFile(filepath.Join(mock, "curl"), []byte(curl), 0o755); err != nil {
				t.Fatal(err)
			}
			script, err := Script(ScriptData{BoxFleetVersion: "v0.14.0", AgentVersion: "v0.8.2"})
			if err != nil {
				t.Fatal(err)
			}
			scriptPath := filepath.Join(dir, "install.sh")
			if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", scriptPath, "bootstrap-secret-test")
			interrupted := "no"
			if interrupt {
				interrupted = "yes"
			}
			cmd.Env = append(os.Environ(), "PATH="+mock+":"+os.Getenv("PATH"), "TMPDIR="+scratch, "BOXFLEET_INSTALL_DIR="+filepath.Join(dir, "install"), "TEST_FIXTURE="+fixture, "TEST_INTERRUPT="+interrupted, "TEST_ARGS="+filepath.Join(dir, "args"), "TEST_BOOTSTRAP="+filepath.Join(dir, "bootstrap"))
			output, err := cmd.CombinedOutput()
			if strings.Contains(string(output), "bootstrap-secret-test") {
				t.Fatal("credential leaked in output")
			}
			if interrupt {
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != 143 {
					t.Fatalf("interruption did not exit 143: %v %s", err, output)
				}
				if !strings.Contains(string(output), "installation terminated") {
					t.Fatalf("missing recovery output: %s", output)
				}
			} else {
				if err != nil {
					t.Fatalf("installation failed: %v %s", err, output)
				}
				args, _ := os.ReadFile(filepath.Join(dir, "args"))
				if string(args) != "bootstrap\n--from-file\n-\n" {
					t.Fatalf("bootstrap exposed in arguments: %s", args)
				}
				received, _ := os.ReadFile(filepath.Join(dir, "bootstrap"))
				if string(received) != "bootstrap-secret-test\n" {
					t.Fatalf("stdin bootstrap lost: %s", received)
				}
			}
			entries, err := os.ReadDir(scratch)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary artifacts retained: %v %v", entries, err)
			}
		})
	}
}
