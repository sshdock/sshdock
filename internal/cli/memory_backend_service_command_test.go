package cli

import (
	"fmt"
	"strings"
)

func (b *MemoryBackend) ExecApp(request ServiceCommandRequest) error {
	return b.validateServiceCommand(request)
}

func (b *MemoryBackend) RunApp(request ServiceCommandRequest) error {
	return b.validateServiceCommand(request)
}

func (b *MemoryBackend) validateServiceCommand(request ServiceCommandRequest) error {
	if _, ok := b.apps[request.AppName]; !ok {
		return fmt.Errorf("app %q not found", request.AppName)
	}
	if strings.TrimSpace(request.ServiceName) == "" {
		return fmt.Errorf("service name is required")
	}
	if len(request.Command) == 0 {
		return fmt.Errorf("service command is required")
	}
	return nil
}
