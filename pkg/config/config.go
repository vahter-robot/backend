package config

import (
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Service  string
	HTTP     http
	MongoDB  mongodb
	Telegram telegram
	LogLevel string
}

type http struct {
	Host string
	Port int
}

type mongodb struct {
	URL string
}

type telegram struct {
	ParentBotToken string
}

func NewConfig() (Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("envconfig.Process: %w", err)
	}
	return cfg, nil
}
