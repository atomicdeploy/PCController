package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/integrationproxy"
)

type configuredIntegrationResolver struct {
	store *appconfig.Store
}

func (resolver configuredIntegrationResolver) ResolveIntegrationTarget(
	_ context.Context,
	name string,
) (integrationproxy.Target, error) {
	if resolver.store == nil {
		return integrationproxy.Target{}, integrationproxy.ErrTargetNotFound
	}
	config := resolver.store.Current().Integrations
	switch name {
	case "datahub":
		if !config.DataHub.Enabled {
			return integrationproxy.Target{}, integrationproxy.ErrTargetNotFound
		}
		target, err := integrationproxy.NormalizeDataHubTarget(
			strings.TrimSpace(config.DataHub.BaseURL),
		)
		if err != nil {
			return integrationproxy.Target{}, fmt.Errorf("data-hub target: %w", err)
		}
		return target, nil
	case "device":
		if !config.LocalDevice.Enabled {
			return integrationproxy.Target{}, integrationproxy.ErrTargetNotFound
		}
		target, err := integrationproxy.NormalizeDeviceTarget(
			strings.TrimSpace(config.LocalDevice.BaseURL),
		)
		if err != nil {
			return integrationproxy.Target{}, fmt.Errorf("local-device target: %w", err)
		}
		return target, nil
	default:
		return integrationproxy.Target{}, integrationproxy.ErrTargetNotFound
	}
}

func newIntegrationProxy(store *appconfig.Store) (http.Handler, error) {
	return integrationproxy.NewHandler(
		"/api/integrations",
		configuredIntegrationResolver{store: store},
	)
}
