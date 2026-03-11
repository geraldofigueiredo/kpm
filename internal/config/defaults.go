package config

var defaultConfig = Config{}

func init() {
	defaultConfig.Defaults.Namespace = "default"
	defaultConfig.Defaults.ReconnectRetries = 3
	defaultConfig.Defaults.ReconnectBackoffSeconds = 2
	defaultConfig.Projects = []Project{}
	defaultConfig.PortOverrides = map[string]int{}
}
