package nordhost

import (
	"testing"

	acmetest "github.com/cert-manager/cert-manager/test/acme"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
)

// TestRunsSuite runs cert-manager's official DNS01 webhook conformance
// suite against the real DirectAdmin account configured via $CONFIG. It
// exercises Present/CleanUp exactly the way cert-manager itself would,
// including polling the domain's authoritative nameservers until the TXT
// record actually appears (and disappears), so it only passes once the
// solver genuinely round-trips through DirectAdmin.
//
// Requires $CONFIG (same as cmd/main) and the envtest control-plane
// binaries set up via `make test`.
func TestRunsSuite(t *testing.T) {
	config, err := cfg.LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if len(config.Domains) == 0 {
		t.Fatal("config has no domains configured")
	}

	solver, err := New(config)
	if err != nil {
		t.Fatalf("failed to construct solver: %v", err)
	}

	zone := config.Domains[0].Domain + "."

	fixture := acmetest.NewFixture(solver,
		acmetest.SetResolvedZone(zone),
		acmetest.SetDNSServer("8.8.8.8:53"),
		acmetest.SetManifestPath("testdata/nordhost-solver"),
		acmetest.SetStrict(true),
	)

	fixture.RunConformance(t)
	fixture.RunBasic(t)
	fixture.RunExtended(t)
}
