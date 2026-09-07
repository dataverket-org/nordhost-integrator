package main

import (
	"log/slog"
	"net/http"
	"os"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
	"git.dataverket.org/tools/nordhost-integrator/extdns"
)

// recordsAddr serves the ExternalDNS webhook provider API. Per the spec
// this should stay localhost-only - ExternalDNS is meant to reach it as a
// sidecar in the same Pod, not over the network.
const defaultRecordsAddr = "127.0.0.1:8888"

// healthAddr serves /healthz (and later /metrics) and, per the spec, should
// be reachable on all interfaces for kubelet probes.
const defaultHealthAddr = "0.0.0.0:8080"

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	config, err := cfg.LoadConfig()
	if err != nil {
		slog.Error("could not load config", slog.String("err", err.Error()))
		os.Exit(1)
	}

	s := extdns.New(config)

	recordsAddr := envOr("RECORDS_ADDR", defaultRecordsAddr)
	healthAddr := envOr("HEALTH_ADDR", defaultHealthAddr)

	errCh := make(chan error, 2)

	go func() {
		slog.Info("serving ExternalDNS webhook provider API", slog.String("addr", recordsAddr))
		errCh <- http.ListenAndServe(recordsAddr, s.RecordsHandler())
	}()

	go func() {
		slog.Info("serving health checks", slog.String("addr", healthAddr))
		errCh <- http.ListenAndServe(healthAddr, extdns.HealthHandler())
	}()

	if err := <-errCh; err != nil {
		slog.Error("server exited", slog.String("err", err.Error()))
		os.Exit(1)
	}
}
