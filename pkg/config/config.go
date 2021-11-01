package config

import (
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Service   string
	HTTP      http
	MongoDB   mongodb
	ParentBot parentBot
	ChildBot  childBot
	LogLevel  string
}

type http struct {
	Host string
}

type mongodb struct {
	URL string
}

type parentBot struct {
	Port            string
	Path            string
	Token           string
	TokenPathPrefix string
}

type childBot struct {
	Port                string
	Path                string
	TokenPathPrefix     string
	BotsLimitPerUser    uint16
	KeywordsLimitPerBot uint16
	InLimitPerKeyword   uint16
	InLimitChars        uint16
	OutLimitChars       uint16
}

func NewConfig() (Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("envconfig.Process: %w", err)
	}
	return cfg, nil
}
