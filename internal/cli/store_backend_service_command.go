package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) ExecApp(request ServiceCommandRequest) error {
	return b.runServiceCommand(request, true)
}

func (b *StoreBackend) RunApp(request ServiceCommandRequest) error {
	return b.runServiceCommand(request, false)
}

func (b *StoreBackend) runServiceCommand(request ServiceCommandRequest, execExisting bool) error {
	if b.recoveryRunner == nil {
		return fmt.Errorf("compose runner is not configured")
	}
	composeRequest := compose.ServiceCommandRequest{
		AppName:     request.AppName,
		ServiceName: request.ServiceName,
		Command:     append([]string(nil), request.Command...),
		TTY:         request.Interactive,
		Stdin:       request.Stdin,
		Stdout:      request.Stdout,
		Stderr:      request.Stderr,
	}
	service := b.recoveryService()
	var err error
	if execExisting {
		err = service.ExecApp(context.Background(), composeRequest)
	} else {
		err = service.RunApp(context.Background(), composeRequest)
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", request.AppName)
	}
	if err != nil {
		return fmt.Errorf("run command for service %q/%q: %w", request.AppName, request.ServiceName, err)
	}
	return nil
}
