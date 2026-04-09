package config

// GeminiConfig holds configuration for the Google Gemini API client.
type GeminiConfig struct {
	APIKey string // Gemini API key (required to enable YouTube summary)
	Model  string // Gemini model name (default: gemini-2.5-flash)
}

type rawGeminiConfig struct {
	APIKey string `koanf:"api_key"`
	Model  string `koanf:"model"`
}

func defaultGeminiConfig() GeminiConfig {
	return GeminiConfig{
		Model: "gemini-2.5-flash",
	}
}

func defaultRawGeminiConfig() rawGeminiConfig {
	cfg := defaultGeminiConfig()
	return rawGeminiConfig{
		Model: cfg.Model,
	}
}

func (r rawGeminiConfig) materialize() (GeminiConfig, error) {
	return GeminiConfig{
		APIKey: r.APIKey,
		Model:  r.Model,
	}, nil
}

func (c GeminiConfig) validate(_ *[]string) {
	// API key is optional — feature is disabled when absent.
}

// Enabled reports whether the Gemini API is configured.
func (c GeminiConfig) Enabled() bool {
	return c.APIKey != ""
}
