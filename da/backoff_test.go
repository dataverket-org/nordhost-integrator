package da

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
)

// fakeDirectAdmin answers every request with status and body, and counts the
// requests that reach it.
func fakeDirectAdmin(t *testing.T, status *atomic.Int32, body *atomic.Value) (*cfg.Domain, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	return &cfg.Domain{Domain: "example.org", Host: srv.URL, Username: "user", Password: "pw"}, &hits
}

// fakeClock replaces now for one test and resets the gates around it.
func fakeClock(t *testing.T) *time.Time {
	t.Helper()
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	prev := now
	now = func() time.Time { return clock }
	gates = sync.Map{}
	t.Cleanup(func() { now = prev; gates = sync.Map{} })
	return &clock
}

func TestRefusalBacksOffAndDoubles(t *testing.T) {
	clock := fakeClock(t)
	var status atomic.Int32
	var body atomic.Value
	status.Store(http.StatusUnauthorized)
	body.Store("error=1&text=bad login")
	domain, hits := fakeDirectAdmin(t, &status, &body)

	if _, err := List(context.Background(), domain); err == nil {
		t.Fatal("expected the refusal as an error")
	}
	_, err := List(context.Background(), domain)
	var backoff *BackoffError
	if !errors.As(err, &backoff) {
		t.Fatalf("expected a BackoffError during the backoff, got %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("DirectAdmin was called %d times during the backoff, want 1", hits.Load())
	}
	if want := clock.Add(backoffBase); !backoff.Until.Equal(want) {
		t.Fatalf("first backoff until %v, want %v", backoff.Until, want)
	}

	*clock = clock.Add(backoffBase + time.Second)
	_, _ = List(context.Background(), domain)
	if hits.Load() != 2 {
		t.Fatalf("DirectAdmin was not called again after the backoff: %d calls", hits.Load())
	}
	_, err = List(context.Background(), domain)
	if !errors.As(err, &backoff) || !backoff.Until.Equal(clock.Add(2*backoffBase)) {
		t.Fatalf("second backoff should double to %v, got %v", 2*backoffBase, err)
	}
}

func TestBlacklistingBacksOffTheLongest(t *testing.T) {
	clock := fakeClock(t)
	var status atomic.Int32
	var body atomic.Value
	status.Store(http.StatusForbidden)
	body.Store("error=1&text=Your+IP+is+blacklisted")
	domain, _ := fakeDirectAdmin(t, &status, &body)

	_, _ = List(context.Background(), domain)
	_, err := List(context.Background(), domain)
	var backoff *BackoffError
	if !errors.As(err, &backoff) || !backoff.Until.Equal(clock.Add(backoffMax)) {
		t.Fatalf("a blacklisting should back off %v at once, got %v", backoffMax, err)
	}
}

func TestSuccessClearsTheBackoff(t *testing.T) {
	clock := fakeClock(t)
	var status atomic.Int32
	var body atomic.Value
	status.Store(http.StatusUnauthorized)
	body.Store("error=1")
	domain, hits := fakeDirectAdmin(t, &status, &body)

	_, _ = List(context.Background(), domain)
	*clock = clock.Add(backoffBase + time.Second)
	status.Store(http.StatusOK)
	body.Store(`{"records":[]}`)
	if _, err := List(context.Background(), domain); err != nil {
		t.Fatalf("expected success after the backoff, got %v", err)
	}
	status.Store(http.StatusUnauthorized)
	body.Store("error=1")
	_, _ = List(context.Background(), domain)
	_, err := List(context.Background(), domain)
	var backoff *BackoffError
	if !errors.As(err, &backoff) || backoff.Failures != 1 || !backoff.Until.Equal(clock.Add(backoffBase)) {
		t.Fatalf("after a success the count should start over, got %v", err)
	}
	if hits.Load() != 3 {
		t.Fatalf("got %d calls, want 3", hits.Load())
	}
}

func TestOtherFailuresDoNotBackOff(t *testing.T) {
	fakeClock(t)
	var status atomic.Int32
	var body atomic.Value
	status.Store(http.StatusInternalServerError)
	body.Store("oops")
	domain, hits := fakeDirectAdmin(t, &status, &body)

	for range 3 {
		_, err := List(context.Background(), domain)
		var backoff *BackoffError
		if errors.As(err, &backoff) {
			t.Fatalf("a 500 is not a refused login: %v", err)
		}
	}
	if hits.Load() != 3 {
		t.Fatalf("got %d calls, want 3", hits.Load())
	}
}
