package cfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

var ErrNoConfig = errors.New("missing config")

type Config struct {
	Domains []Domain `json:"domains"`
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
