package logger

import (
	"errors"
	"github.com/rs/zerolog"
	"os"
)

func NewLogger(logLevel string) (zerolog.Logger, error) {
	if logLevel == "" {
		return zerolog.Logger{}, errors.New("empty level")
	}

	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		return zerolog.Logger{}, err
	}

	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(level).With().Timestamp().Caller().Logger(), nil
}
