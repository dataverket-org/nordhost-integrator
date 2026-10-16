package da

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DirectAdmin's brute force monitor blacklists the address a client calls
// from after repeated failed logins, and from then on refuses every request
// from it, with valid credentials too, until an administrator lifts it. A
// webhook that retries a refused key every minute walks straight into that,
// as dataverket-prod did on 2026-10-15. So once DirectAdmin refuses
// credentials, a host is not called again until a backoff has passed, which
// doubles with each refusal; a blacklisting goes to the longest backoff at
// once. Network errors and other statuses pass through untouched: retrying
// those does not count as a failed login.

var (
	backoffBase = 2 * time.Minute
	backoffMax  = time.Hour
	now         = time.Now
)

// BackoffError is returned instead of calling a DirectAdmin host that
// refused credentials, until Until.
type BackoffError struct {
	Host     string
	Until    time.Time
	Failures int
	Reason   string
}

func (e *BackoffError) Error() string {
	return fmt.Sprintf("not calling DirectAdmin at %s until %s after %d refused logins: %s",
		e.Host, e.Until.UTC().Format(time.RFC3339), e.Failures, e.Reason)
}

type gate struct {
	mu       sync.Mutex
	failures int
	until    time.Time
	reason   string
}

var gates sync.Map // host -> *gate

func gateFor(host string) *gate {
	g, _ := gates.LoadOrStore(host, &gate{})
	return g.(*gate)
}

// check returns a BackoffError while the host is backed off.
func (g *gate) check(host string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now().Before(g.until) {
		return &BackoffError{Host: host, Until: g.until, Failures: g.failures, Reason: g.reason}
	}
	return nil
}

// refused records a refusal and backs the host off.
func (g *gate) refused(host, user string, status int, body []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures++
	g.reason = fmt.Sprintf("status %d: %s", status, firstLine(body))
	wait := backoffMax
	if !strings.Contains(string(body), "blacklisted") {
		wait = backoffBase << (g.failures - 1)
		if wait <= 0 || wait > backoffMax {
			wait = backoffMax
		}
	}
	g.until = now().Add(wait)
	slog.Warn("DirectAdmin refused credentials; backing off",
		slog.String("host", host), slog.String("user", user), slog.Int("status", status),
		slog.Int("failures", g.failures), slog.Duration("wait", wait), slog.String("reason", g.reason))
}

// succeeded clears the backoff after a request DirectAdmin answered.
func (g *gate) succeeded(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failures > 0 {
		slog.Info("DirectAdmin answers again", slog.String("host", host), slog.Int("after_failures", g.failures))
	}
	g.failures = 0
	g.until = time.Time{}
	g.reason = ""
}

func firstLine(body []byte) string {
	s := strings.TrimSpace(string(body))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
