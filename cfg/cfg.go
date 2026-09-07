package cfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

var ErrNoConfig = errors.New("missing config")

type Config struct {
	Domains []Domain `json:"domains"`
}

// FindDomain returns the configured Domain matching zone exactly (zone may
// have a trailing dot, e.g. "example.com."), or nil if none is configured.
func (c *Config) FindDomain(zone string) *Domain {
	zone = strings.TrimSuffix(zone, ".")
	for i, d := range c.Domains {
		if d.Domain == zone {
			return &c.Domains[i]
		}
	}
	return nil
}

// FindDomainForName returns the configured Domain whose zone is name itself
// or an ancestor of it (e.g. "www.example.com" matches "example.com"),
// preferring the longest (most specific) match, or nil if none configured.
func (c *Config) FindDomainForName(name string) *Domain {
	name = strings.TrimSuffix(name, ".")
	var best *Domain
	for i, d := range c.Domains {
		if name != d.Domain && !strings.HasSuffix(name, "."+d.Domain) {
			continue
		}
		if best == nil || len(d.Domain) > len(best.Domain) {
			best = &c.Domains[i]
		}
	}
	return best
}

func load(r io.Reader) (*Config, error) {
	decoder := json.NewDecoder(r)

	config := new(Config)
	err := decoder.Decode(config)
	return config, err
}

func LoadConfig() (*Config, error) {
	val := os.Getenv("CONFIG")
	if val == "" {
		return nil, ErrNoConfig
	}

	_, err := os.Stat(val)
	if errors.Is(err, os.ErrNotExist) {
		return load(bytes.NewReader([]byte(val)))
	}

	file, err := os.Open(val)
	if err != nil {
		return nil, err
	}

	config, err := load(file)
	file.Close()

	return config, err
}
