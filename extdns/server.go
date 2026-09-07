// Package extdns implements the ExternalDNS webhook provider HTTP contract
// (https://github.com/kubernetes-sigs/external-dns/blob/master/docs/tutorials/webhook-provider.md)
// against DirectAdmin, via the da package.
package extdns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
	"git.dataverket.org/tools/nordhost-integrator/da"
	"github.com/miekg/dns"
)

// mediaType is the only content type version this provider supports.
const mediaType = "application/external.dns.webhook+json;version=1"

var errNoDomain = errors.New("no configured domain for name")

// ProviderSpecificProperty is a provider-specific key/value pair attached to
// an Endpoint.
type ProviderSpecificProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Endpoint is a single DNS record as described by the ExternalDNS webhook
// provider contract.
type Endpoint struct {
	DNSName          string                     `json:"dnsName"`
	Targets          []string                   `json:"targets,omitempty"`
	RecordType       string                     `json:"recordType"`
	SetIdentifier    string                     `json:"setIdentifier,omitempty"`
	RecordTTL        int64                      `json:"recordTTL,omitempty"`
	Labels           map[string]string          `json:"labels,omitempty"`
	ProviderSpecific []ProviderSpecificProperty `json:"providerSpecific,omitempty"`
}

// Changes is the payload ExternalDNS sends to POST /records.
type Changes struct {
	Create    []Endpoint `json:"create,omitempty"`
	UpdateOld []Endpoint `json:"updateOld,omitempty"`
	UpdateNew []Endpoint `json:"updateNew,omitempty"`
	Delete    []Endpoint `json:"delete,omitempty"`
}

type filters struct {
	Filters []string `json:"filters"`
}

// Server implements the ExternalDNS webhook provider contract against the
// domains configured in c.
type Server struct {
	c *cfg.Config
}

func New(config *cfg.Config) *Server {
	return &Server{c: config}
}

// RecordsHandler serves the provider API (/, /records, /adjustendpoints).
// Per the ExternalDNS spec this should be bound to 127.0.0.1 only, as it is
// meant to run as a localhost-only sidecar to the ExternalDNS container.
func (s *Server) RecordsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleNegotiate)
	mux.HandleFunc("GET /records", s.handleGetRecords)
	mux.HandleFunc("POST /records", s.handleApplyChanges)
	mux.HandleFunc("POST /adjustendpoints", s.handleAdjustEndpoints)
	return mux
}

// HealthHandler serves /healthz. Per the spec this should be bound to
// 0.0.0.0, unlike RecordsHandler.
func HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (s *Server) handleNegotiate(w http.ResponseWriter, r *http.Request) {
	f := filters{Filters: make([]string, len(s.c.Domains))}
	for i, d := range s.c.Domains {
		f.Filters[i] = d.Domain
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleGetRecords(w http.ResponseWriter, r *http.Request) {
	endpoints := []Endpoint{}
	for i := range s.c.Domains {
		domain := &s.c.Domains[i]
		records, err := da.List(r.Context(), domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		endpoints = append(endpoints, recordsToEndpoints(domain.Domain, records)...)
	}
	writeJSON(w, http.StatusOK, endpoints)
}

// recordsToEndpoints groups DirectAdmin records by (name, type) into
// Endpoints, combining same-name-and-type records (e.g. round-robin A
// records) into a single Endpoint with multiple targets.
func recordsToEndpoints(domain string, records []da.Record) []Endpoint {
	type key struct{ name, typ string }

	grouped := make(map[key]*Endpoint, len(records))
	order := make([]key, 0, len(records))

	for _, r := range records {
		k := key{name: r.Name, typ: r.Type}
		ep, ok := grouped[k]
		if !ok {
			ep = &Endpoint{
				DNSName:    fqdn(r.Name, domain),
				RecordType: r.Type,
			}
			grouped[k] = ep
			order = append(order, k)
		}
		ep.Targets = append(ep.Targets, r.Value)
		if ttl, err := strconv.ParseInt(r.TTL, 10, 64); err == nil {
			ep.RecordTTL = ttl
		}
	}

	endpoints := make([]Endpoint, 0, len(order))
	for _, k := range order {
		endpoints = append(endpoints, *grouped[k])
	}
	return endpoints
}

// fqdn converts a DirectAdmin-relative record name into a fully-qualified,
// non-trailing-dot DNS name. DirectAdmin represents the apex record's name
// as "<domain>." rather than an empty name or "@" - which itself, once
// trailing-dot-trimmed, equals domain (not a "."+domain suffix), so it
// needs its own check rather than relying on cfg.RecordName reducing it to
// "".
func fqdn(name, domain string) string {
	if strings.TrimSuffix(name, ".") == domain {
		return domain
	}
	return cfg.RecordName(name, domain) + "." + domain
}

func (s *Server) handleApplyChanges(w http.ResponseWriter, r *http.Request) {
	var changes Changes
	if err := json.NewDecoder(r.Body).Decode(&changes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	ctx := r.Context()

	for _, ep := range changes.Create {
		if err := s.createEndpoint(ctx, ep); err != nil {
			http.Error(w, err.Error(), statusFor(err))
			return
		}
	}
	for _, ep := range changes.Delete {
		if err := s.deleteEndpoint(ctx, ep); err != nil {
			http.Error(w, err.Error(), statusFor(err))
			return
		}
	}
	for i := range changes.UpdateOld {
		var newEP Endpoint
		if i < len(changes.UpdateNew) {
			newEP = changes.UpdateNew[i]
		}
		if err := s.updateEndpoint(ctx, changes.UpdateOld[i], newEP); err != nil {
			http.Error(w, err.Error(), statusFor(err))
			return
		}
	}

	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdjustEndpoints(w http.ResponseWriter, r *http.Request) {
	var endpoints []Endpoint
	if err := json.NewDecoder(r.Body).Decode(&endpoints); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// No provider-specific adjustments needed: pass through unchanged.
	writeJSON(w, http.StatusOK, endpoints)
}

func statusFor(err error) int {
	if errors.Is(err, errNoDomain) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// domainAndName resolves dnsName to its configured Domain and the
// DirectAdmin-relative record name within that domain's zone. Apex records
// (name == domain) are passed through as "<domain>.", matching how
// DirectAdmin itself represents them in da.List results - this is inferred
// from List's output, not independently verified against Create/Delete.
func (s *Server) domainAndName(dnsName string) (*cfg.Domain, string, error) {
	dom := s.c.FindDomainForName(dnsName)
	if dom == nil {
		return nil, "", fmt.Errorf("%w: %s", errNoDomain, dnsName)
	}

	if strings.TrimSuffix(dnsName, ".") == dom.Domain {
		return dom, dom.Domain + ".", nil
	}
	return dom, cfg.RecordName(dnsName, dom.Domain), nil
}

func (s *Server) createEndpoint(ctx context.Context, ep Endpoint) error {
	dom, name, err := s.domainAndName(ep.DNSName)
	if err != nil {
		return err
	}
	typ := dns.Type(dns.StringToType[ep.RecordType])
	for _, target := range ep.Targets {
		if err := da.Create(ctx, dom, typ, name, target); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) deleteEndpoint(ctx context.Context, ep Endpoint) error {
	dom, name, err := s.domainAndName(ep.DNSName)
	if err != nil {
		return err
	}
	typ := dns.Type(dns.StringToType[ep.RecordType])
	for _, target := range ep.Targets {
		if err := da.Delete(ctx, dom, typ, name, target); err != nil {
			return err
		}
	}
	return nil
}

// updateEndpoint diffs oldEP/newEP targets and only touches what changed:
// deletes targets that were removed, creates targets that were added.
func (s *Server) updateEndpoint(ctx context.Context, oldEP, newEP Endpoint) error {
	dom, name, err := s.domainAndName(newEP.DNSName)
	if err != nil {
		return err
	}
	typ := dns.Type(dns.StringToType[newEP.RecordType])

	oldTargets := make(map[string]bool, len(oldEP.Targets))
	for _, t := range oldEP.Targets {
		oldTargets[t] = true
	}
	newTargets := make(map[string]bool, len(newEP.Targets))
	for _, t := range newEP.Targets {
		newTargets[t] = true
	}

	for target := range oldTargets {
		if !newTargets[target] {
			if err := da.Delete(ctx, dom, typ, name, target); err != nil {
				return err
			}
		}
	}
	for target := range newTargets {
		if !oldTargets[target] {
			if err := da.Create(ctx, dom, typ, name, target); err != nil {
				return err
			}
		}
	}
	return nil
}
