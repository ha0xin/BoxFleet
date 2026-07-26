package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/haoxin/boxfleet/internal/agent"
)

// bootstrapEnvVar carries the bootstrap string without exposing the node token
// in the process arguments.
const bootstrapEnvVar = "BOXFLEET_BOOTSTRAP"

var version = "dev"

func main() {
	agent.Version = version
	if len(os.Args) < 2 {
		printUsage()
		return
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "bootstrap":
		runBootstrapCommand()
	case "check":
		runAgentCommand("check", func(ctx context.Context, a *agent.Agent) error {
			return a.Check(ctx)
		})
	case "install":
		runAgentCommand("install", func(ctx context.Context, a *agent.Agent) error {
			return a.Install(ctx)
		})
	case "once":
		runAgentCommand("once", func(ctx context.Context, a *agent.Agent) error {
			return a.Once(ctx)
		})
	case "run":
		runAgentCommand("run", func(ctx context.Context, a *agent.Agent) error {
			return a.Run(ctx)
		})
	case "guard":
		runAgentCommand("guard", func(_ context.Context, a *agent.Agent) error {
			return a.RunAgentGuard()
		})
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "boxfleet-agent: unknown command %q\n", os.Args[1])
		os.Exit(1)
	}
}

func runBootstrapCommand() {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	fromFile := fs.String("from-file", "", `read the bootstrap string from a file ("-" for stdin)`)
	allowInsecure := fs.Bool("allow-insecure-transport", false, "permit plaintext http server and sing-box URLs (development only)")
	_ = fs.Parse(os.Args[2:])
	value, err := bootstrapValue(fs, *fromFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent bootstrap: %v\n", err)
		os.Exit(1)
	}
	if err := agent.Bootstrap(context.Background(), value, *allowInsecure); err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent bootstrap: %v\n", err)
		os.Exit(1)
	}
}

// bootstrapValue prefers sources that keep the node token out of the process
// arguments, which are readable through ps and land in shell history.
func bootstrapValue(fs *flag.FlagSet, fromFile string) (string, error) {
	sources := 0
	if fromFile != "" {
		sources++
	}
	if os.Getenv(bootstrapEnvVar) != "" {
		sources++
	}
	sources += fs.NArg()
	if sources != 1 {
		return "", fmt.Errorf("bootstrap requires exactly one of %s, --from-file, or a bootstrap string argument", bootstrapEnvVar)
	}
	switch {
	case fromFile == "-":
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	case fromFile != "":
		raw, err := os.ReadFile(fromFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	case fs.NArg() == 1:
		return fs.Arg(0), nil
	default:
		return strings.TrimSpace(os.Getenv(bootstrapEnvVar)), nil
	}
}

func runAgentCommand(name string, fn func(context.Context, *agent.Agent) error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	configPath := fs.String("config", agent.DefaultConfigPath, "agent config path")
	_ = fs.Parse(os.Args[2:])
	config, err := agent.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent %s: %v\n", name, err)
		os.Exit(1)
	}
	if err := fn(context.Background(), agent.New(config)); err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent %s: %v\n", name, err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`boxfleet-agent runs on proxy nodes.

Usage:
  BOXFLEET_BOOTSTRAP=<boxfleet-bootstrap:string> boxfleet-agent bootstrap
  boxfleet-agent bootstrap --from-file <path|->
  boxfleet-agent bootstrap <boxfleet-bootstrap:string>  (discouraged: the node
      token is visible in ps and saved in shell history)
  boxfleet-agent install [--config /etc/boxfleet/agent.json]
  boxfleet-agent run [--config /etc/boxfleet/agent.json]
  boxfleet-agent guard [--config /etc/boxfleet/agent.json]
  boxfleet-agent once [--config /etc/boxfleet/agent.json]
  boxfleet-agent check [--config /etc/boxfleet/agent.json]
  boxfleet-agent version`)
}
