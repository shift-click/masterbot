package config

// ChartConfig holds configuration for the chart renderer sidecar.
type ChartConfig struct {
	RendererURL string // HTTP URL of the chart-renderer sidecar (e.g. "http://jucobot-chart-renderer:3100")
}

type rawChartConfig struct {
	RendererURL string `koanf:"renderer_url"`
}

func defaultChartConfig() ChartConfig {
	return ChartConfig{}
}

func defaultRawChartConfig() rawChartConfig {
	return rawChartConfig{}
}

func (r rawChartConfig) materialize() (ChartConfig, error) {
	return ChartConfig{
		RendererURL: r.RendererURL,
	}, nil
}

func (c ChartConfig) validate(_ *[]string) {
	// Chart config is optional; empty values intentionally mean "disabled".
}

// Enabled reports whether the chart renderer sidecar is configured.
func (c ChartConfig) Enabled() bool {
	return c.RendererURL != ""
}
