package cmd

// strspcConfig mirrors the fields from .strspc/config.yaml used by multiple commands.
type strspcConfig struct {
	Rules []struct {
		Source string `yaml:"source"`
		Scope  string `yaml:"scope"`
	} `yaml:"rules"`
	Cache struct {
		TTL string `yaml:"ttl"`
	} `yaml:"cache"`
	Evaluator struct {
		Provider string `yaml:"provider"`
		Endpoint string `yaml:"endpoint"`
		Model    string `yaml:"model"`
	} `yaml:"evaluator"`
	FailOn []string `yaml:"fail_on"`
}
