package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/session"
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
	reply := protocol.Success(result)
	if err != nil {
		reply = protocol.Failure(err)
		exitCode = 1
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if encodeErr := encoder.Encode(reply); encodeErr != nil {
		fmt.Fprintln(os.Stderr, "encode response:", encodeErr)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

func run(args []string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current, err := session.New()
	if err != nil {
		return nil, err
	}
	if err := current.Check(ctx); err != nil {
		return nil, err
	}
	return current.Run(ctx, args)
}
