package main

import (
	"context"

	"github.com/sshdock/sshdock/internal/config"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/store"
)

func configRecoveryHost(ctx context.Context, persistentStore store.Store, cfg config.Config) string {
	if serverConfig, err := persistentStore.GetServerConfig(ctx); err == nil {
		if serverConfig.BaseDomain != "" {
			return domaincfg.ControlHost(serverConfig.BaseDomain)
		}
		if serverConfig.GitHost != "" {
			return serverConfig.GitHost
		}
	}
	return cfg.GitHost
}
