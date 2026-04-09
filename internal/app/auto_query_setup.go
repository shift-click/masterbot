package app

import (
	"context"
	"fmt"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
)

// initAutoQueryManager isolates auto-query bootstrap wiring from Build.
func initAutoQueryManager(cfg config.Config, catalog *intent.Catalog, stateStore store.Store) (*bot.AutoQueryManager, error) {
	autoQueryStore := bot.NewAutoQueryStore(stateStore)
	autoQueryManager := bot.NewAutoQueryManager(catalog, autoQueryStore, autoQueryPolicyFromConfig(cfg.AutoQuery.DefaultPolicy, catalog))
	if err := autoQueryManager.Bootstrap(context.Background(), autoQueryRoomsFromConfig(cfg.AutoQuery.Rooms, catalog)); err != nil {
		return nil, fmt.Errorf("bootstrap auto query policy: %w", err)
	}
	return autoQueryManager, nil
}
