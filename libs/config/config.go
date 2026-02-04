package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type HTTPConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
}

type AppConfig struct {
	ServiceName string     `mapstructure:"service_name"`
	Env         string     `mapstructure:"env"`
	LogLevel    string     `mapstructure:"log_level"`
	MetricsPath string     `mapstructure:"metrics_path"`
	HTTP        HTTPConfig `mapstructure:"http"`
}

func Load(path string) (*AppConfig, error) {
	v := viper.New()
	v.SetEnvPrefix("CEX")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	if path == "" {
		path = "config.yaml"
	}

	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("service_name", "template-service")
	v.SetDefault("env", "dev")
	v.SetDefault("log_level", "info")
	v.SetDefault("metrics_path", "/metrics")
	v.SetDefault("http.host", "0.0.0.0")
	v.SetDefault("http.port", 8080)
	v.SetDefault("http.read_timeout", "5s")
	v.SetDefault("http.write_timeout", "10s")
	v.SetDefault("http.idle_timeout", "60s")
}
