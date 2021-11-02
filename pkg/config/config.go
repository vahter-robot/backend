package config

import (
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Service            string
	MongoDB            mongodb
	ParentBot          parentBot
	ChildBot           childBot
	SetWebhooksOnStart bool
	LogLevel           string
}

type mongodb struct {
	URL string
}

type parentBot struct {
	Host            string
	Port            string
	Token           string
	TokenPathPrefix string
}

type childBot struct {
	Host                string
	Port                string
	TokenPathPrefix     string
	BotsLimitPerUser    uint16
	KeywordsLimitPerBot uint16
	InLimitPerKeyword   uint16
	InLimitChars        uint16
	OutLimitChars       uint16
	TimeoutOnHandle     bool
}

func NewConfig() (Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("envconfig.Process: %w", err)
	}
	return cfg, nil
}
