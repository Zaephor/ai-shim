package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile loads a Config from a YAML file. Returns an empty Config if the file
// does not exist (not an error — missing tiers are normal).
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if len(data) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
