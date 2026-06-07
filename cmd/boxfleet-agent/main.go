package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/haoxin/boxfleet/internal/agent"
)

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
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "boxfleet-agent: unknown command %q\n", os.Args[1])
		os.Exit(1)
	}
}

func runBootstrapCommand() {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	_ = fs.Parse(os.Args[2:])
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "boxfleet-agent bootstrap requires one bootstrap string")
		os.Exit(1)
	}
	if err := agent.Bootstrap(context.Background(), fs.Arg(0)); err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent bootstrap: %v\n", err)
		os.Exit(1)
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
  boxfleet-agent bootstrap <boxfleet-bootstrap:string>
  boxfleet-agent install [--config /etc/boxfleet/agent.json]
  boxfleet-agent run [--config /etc/boxfleet/agent.json]
  boxfleet-agent once [--config /etc/boxfleet/agent.json]
  boxfleet-agent check [--config /etc/boxfleet/agent.json]
  boxfleet-agent version`)
}
