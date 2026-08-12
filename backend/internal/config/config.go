package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	APIAddress   string
	DatabaseURL  string
	CookieSecure bool
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

	cookieSecure := false
	if value := os.Getenv("COOKIE_SECURE"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse COOKIE_SECURE: %w", err)
		}
		cookieSecure = parsed
	}

	return Config{
		APIAddress:   address,
		DatabaseURL:  databaseURL,
		CookieSecure: cookieSecure,
	}, nil
}
