package da

import (
	"context"
	"testing"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
)

// TestList checks List against the real DirectAdmin account configured via
// $CONFIG. It skips if $CONFIG isn't set, same convention as the webhook
// conformance test.
func TestList(t *testing.T) {
	config, err := cfg.LoadConfig()
	if err != nil {
		t.Skipf("skipping: no config available: %v", err)
	}
	if len(config.Domains) == 0 {
		t.Skip("skipping: config has no domains configured")
	}

	domain := &config.Domains[0]

	records, err := List(context.Background(), domain)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one record, got none")
	}

	var sawA bool
	for _, r := range records {
		if r.Type == "" || r.Name == "" {
			t.Errorf("record missing type/name: %+v", r)
		}
		if r.Type == "A" {
			sawA = true
		}
	}
	if !sawA {
		t.Errorf("expected at least one A record among %d records", len(records))
	}
}
