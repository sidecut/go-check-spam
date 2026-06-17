// Package config loads application settings from CLI flags and environment variables.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config holds the runtime settings for the spam checker.
type Config struct {
	Timeout      int
	InitialDelay int
	Days         int
	Debug        bool
	Concurrency  int
	OAuthPort    int
}

// Load reads flags and environment variables and returns a populated Config.
// Environment variables use the prefix GOCHECKSPAM_ (e.g. GOCHECKSPAM_DAYS).
func Load() (*Config, error) {
	pflag.Int("timeout", 60, "timeout in seconds")
	pflag.Int("initial-delay", 1000, "max initial delay in milliseconds before starting to fetch messages")
	pflag.Int("days", 30, "number of days to look back")
	pflag.Bool("debug", false, "enable debug output")
	pflag.Int("concurrency", 8, "number of concurrent workers fetching messages")
	pflag.Int("oauth-port", 8080, "port for local OAuth callback server")
	pflag.Parse()

	viper.SetEnvPrefix("GOCHECKSPAM")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("failed to bind flags: %w", err)
	}

	return &Config{
		Timeout:      viper.GetInt("timeout"),
		InitialDelay: viper.GetInt("initial-delay"),
		Days:         viper.GetInt("days"),
		Debug:        viper.GetBool("debug"),
		Concurrency:  viper.GetInt("concurrency"),
		OAuthPort:    viper.GetInt("oauth-port"),
	}, nil
}
