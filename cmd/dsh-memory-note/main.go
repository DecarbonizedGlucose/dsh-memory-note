package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/decglu/dsh-memory-note/internal/rpc"
	"github.com/decglu/dsh-memory-note/internal/session"
)

const version = "1.0"

const usageText = `usage: dsh-memory-note <subcommand> <json request>

subcommands:

    memory-search
    memory-get
    memory-create
    memory-update
    memory-supersede
    memory-invalidate
    memory-delete
    workspace-register
    workspace-rebind
    workspace-clear
    workspace-delete

    version
    help
`

func main() {
	exitCode := 0
	if len(os.Args) == 2 {
		if os.Args[1] == "version" {
			fmt.Fprintln(os.Stderr, version)
			return
		}
		if os.Args[1] == "help" {
			fmt.Fprint(os.Stderr, usageText)
			return
		}
	}
	if len(os.Args) != 3 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(1)
	}

	result, err := run(os.Args[1:])
	reply := rpc.Success(result)
	if err != nil {
		reply = rpc.Failure(err)
		exitCode = 1
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if encodeErr := encoder.Encode(reply); encodeErr != nil {
		fmt.Fprintln(os.Stderr, "encode response:", encodeErr)
		os.Exit(1)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func run(args []string) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("usage: dsh-memory-note <subcommand> '<json request>'")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current, err := session.New()
	if err != nil {
		return nil, err
	}
	if err := current.Check(ctx); err != nil {
		return nil, err
	}
	server := current.Server

	switch args[0] {
	// Each public function has one subcommand and one JSON protocol.
	case "memory-search":
		return server.MemorySearch(ctx, args[1])
	case "memory-get":
		return server.MemoryGet(ctx, args[1])
	case "memory-create":
		return server.MemoryCreate(ctx, args[1])
	case "memory-update":
		return server.MemoryUpdate(ctx, args[1])
	case "memory-supersede":
		return server.MemorySupersede(ctx, args[1])
	case "memory-invalidate":
		return server.MemoryInvalidate(ctx, args[1])
	case "memory-delete":
		return server.MemoryDelete(ctx, args[1])
	case "workspace-register":
		return server.WorkspaceRegister(ctx, args[1])
	case "workspace-rebind":
		return server.WorkspaceRebind(ctx, args[1])
	case "workspace-clear":
		return server.WorkspaceClear(ctx, args[1])
	case "workspace-delete":
		return server.WorkspaceDelete(ctx, args[1])
	default:
		return nil, fmt.Errorf("unknown subcommand %q", args[0])
	}
}
