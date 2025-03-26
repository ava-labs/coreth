package manipulation

type Config struct {
	Enabled           bool     `json:"enabled"`
	CensorshipEnabled bool     `json:"censorshipEnabled,omitempty"`
	CensoredAddresses []string `json:"censored_addresses,omitempty"`
	PriorityAddresses []string `json:"priority_addresses,omitempty"`
	DetectInjection   bool     `json:"detect_injection,omitempty"`
}

func NewDefaultConfig() *Config {
	return &Config{
		Enabled:           false,
		CensorshipEnabled: false,
		CensoredAddresses: []string{},
		PriorityAddresses: []string{},
		DetectInjection:   false,
	}
}
