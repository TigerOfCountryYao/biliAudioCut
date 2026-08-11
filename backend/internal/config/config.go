package config

import (
	"fmt"
	"os"
)

type Config struct {
	APIAddress  string
	DatabaseURL string
}

func Load() (Config, error) {
	address := os.Getenv("API_ADDR")
	if address == "" {
		address = ":8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return Config{
		APIAddress:  address,
		DatabaseURL: databaseURL,
	}, nil
}
