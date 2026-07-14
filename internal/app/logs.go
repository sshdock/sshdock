package app

import (
	"context"
	"fmt"

	"github.com/sshdock/sshdock/internal/compose"
)

func (s *Service) Logs(ctx context.Context, appName string, serviceName string, lines int) (string, error) {
	if s.logs == nil {
		return "", fmt.Errorf("logs runner is not configured")
	}

	return s.logs.Logs(ctx, compose.LogsRequest{AppName: appName, ServiceName: serviceName, Lines: lines})
}
