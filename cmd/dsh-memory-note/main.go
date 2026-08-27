package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/session"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/version"
)

const usageText = `usage: dsh-memory-note <subcommand> <json request>

subcommands:
    workspace-resolve
    workspace-register
    workspace-rebind
    workspace-clear
    workspace-delete
    memory-search
    memory-list
    memory-get
    memory-history
    memory-diff
    memory-create
    memory-update
    memory-supersede
    memory-invalidate
    memory-delete

    version
    help
`

func main() {
	if len(os.Args) == 2 {
		if os.Args[1] == "version" {
			fmt.Println(version.Version)
			return
		}
		if os.Args[1] == "help" {
			fmt.Fprint(os.Stderr, usageText)
			return
		}
	}
	if len(os.Args) != 3 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}

	reply := run(os.Args[1], os.Args[2])
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if encodeErr := encoder.Encode(reply); encodeErr != nil {
		fmt.Fprintln(os.Stderr, "encode response:", encodeErr)
		os.Exit(2)
	}
	if !reply.OK {
		os.Exit(1)
	}
}

// run creates the Session for this invocation, executes exactly one
// subcommand, and removes the Session's temporary directory before the
// response is emitted.
func run(name, raw string) protocol.Response {
	current, err := session.New()
	if err != nil {
		return protocol.Failure(err)
	}
	defer current.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return current.Run(ctx, name, raw)
}
