// Package command implements the public one-shot commands.
package command

import (
	"context"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

// Run dispatches exactly one subcommand. sessionID is the owning Session's
// ID; every workspace lock acquired by the command is recorded under it.
func Run(ctx context.Context, storeRoot, sessionID, name, raw string) protocol.Response {
	var data any
	var err error
	switch name {
	case "workspace-resolve":
		data, err = workspaceResolve(ctx, storeRoot, sessionID, raw)
	case "workspace-register":
		data, err = workspaceRegister(ctx, storeRoot, sessionID, raw)
	case "workspace-rebind":
		data, err = workspaceRebind(ctx, storeRoot, sessionID, raw)
	case "workspace-clear":
		data, err = workspaceClear(ctx, storeRoot, sessionID, raw)
	case "workspace-delete":
		data, err = workspaceDelete(ctx, storeRoot, sessionID, raw)
	case "memory-search":
		data, err = memorySearch(ctx, storeRoot, sessionID, raw)
	case "memory-list":
		data, err = memoryList(ctx, storeRoot, sessionID, raw)
	case "memory-get":
		data, err = memoryGet(ctx, storeRoot, sessionID, raw)
	case "memory-create":
		data, err = memoryCreate(ctx, storeRoot, sessionID, raw)
	case "memory-update":
		data, err = memoryUpdate(ctx, storeRoot, sessionID, raw)
	case "memory-supersede":
		data, err = memorySupersede(ctx, storeRoot, sessionID, raw)
	case "memory-invalidate":
		data, err = memoryInvalidate(ctx, storeRoot, sessionID, raw)
	case "memory-delete":
		data, err = memoryDelete(ctx, storeRoot, sessionID, raw)
	default:
		err = protocol.Invalid("unknown subcommand")
	}
	if err != nil {
		return protocol.Failure(err)
	}
	return protocol.Success(data)
}
