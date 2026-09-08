package nordhost

import (
	"context"
	"testing"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
	"git.dataverket.org/tools/nordhost-integrator/da"
)

// TestCleanUpActuallyDeletes exists because cert-manager's own conformance
// suite (TestExtendedDeletingOneRecordRetainsOthers) only asserts that
// CleanUp returns a nil error - it never checks the record actually
// disappeared. That gap let da.Delete ship broken (using the wrong
// DirectAdmin API shape) for a long time without any test catching it: DA
// returned 200 OK for a delete request it silently never acted on. This
// test checks real ground truth via da.List instead of trusting a nil
// error.
func TestCleanUpActuallyDeletes(t *testing.T) {
	config, err := cfg.LoadConfig()
	if err != nil {
		t.Skipf("skipping: no config available: %v", err)
	}
	if len(config.Domains) == 0 {
		t.Skip("skipping: config has no domains configured")
	}

	solver, err := New(config)
	if err != nil {
		t.Fatalf("failed to construct solver: %v", err)
	}

	domain := config.Domains[0].Domain
	ctx := context.Background()

	ch := &v1alpha1.ChallengeRequest{
		ResolvedZone: domain + ".",
		ResolvedFQDN: "_cleanup-regression-test." + domain + ".",
		Key:          "cleanup-regression-value",
	}

	if err := solver.Present(ch); err != nil {
		t.Fatalf("Present failed: %v", err)
	}

	if !hasRecord(t, ctx, &config.Domains[0], "_cleanup-regression-test", "cleanup-regression-value") {
		t.Fatal("record was not present after Present")
	}

	if err := solver.CleanUp(ch); err != nil {
		t.Fatalf("CleanUp failed: %v", err)
	}

	if hasRecord(t, ctx, &config.Domains[0], "_cleanup-regression-test", "cleanup-regression-value") {
		t.Fatal("record still present after CleanUp - CleanUp returned nil but did not actually delete it")
	}
}

func hasRecord(t *testing.T, ctx context.Context, domain *cfg.Domain, name, value string) bool {
	t.Helper()
	records, err := da.List(ctx, domain)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, r := range records {
		if r.Name == name && r.Value == value {
			return true
		}
	}
	return false
}
