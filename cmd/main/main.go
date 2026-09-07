package main

import (
	"log/slog"

	nordhost "git.dataverket.org/tools/nordhost-integrator"
	"git.dataverket.org/tools/nordhost-integrator/cfg"
)

func main() {
	config, err := cfg.LoadConfig()
	if err != nil {
		slog.Error("could not load config", slog.String("err", err.Error()))
		return
	}

	s, err := nordhost.New(config)
	if err != nil {
		slog.Error("could not set up nordhost integrator", slog.String("err", err.Error()))
		return
	}

	s.Run()
}
