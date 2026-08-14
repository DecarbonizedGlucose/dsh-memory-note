package main

import (
	"fmt"
	"os"
)

const version = "1.0"

const usageText = `\
usage: dsh-memory-note <subcommand> [<json request>]
`

func main() {
	switch len(os.Args) {
	case 1:
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(1)
	case 2:
		if os.Args[1] == "version" {
			fmt.Fprintln(os.Stderr, version)
			return
		}
		if os.Args[1] == "help" {
			fmt.Fprint(os.Stderr, usageText)
			return
		}
	default:
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(1)
	}

	// TODO: handle the subcommand and JSON arg
}
