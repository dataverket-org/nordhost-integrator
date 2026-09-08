package da

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
	"github.com/miekg/dns"
)

// This package encapsulates the DirectAdmin Evolution-skin DNS control API
// for programatically defining DNS records.

type (
	CMD    = string
	Action = string
)

const (
	CMDDNSControl CMD = "CMD_DNS_CONTROL"

	ActionAdd Action = "add"

	// DirectAdmin's DNS delete uses "select" (checkbox-selection semantics
	// from its legacy API), not "delete" - see Delete below. Confirmed
	// empirically: "delete"/"del"/"remove"/etc. are all silently ignored
	// and fall back to a list response; only "select" is recognized.
	ActionSelect Action = "select"
)

type dnsControlRequest struct {
	Domain         string `json:"domain"`
	Type           string `json:"type"`
	Name           string `json:"name"`
	Value          string `json:"value"`
	TTL            string `json:"ttl"`
	AffectPointers string `json:"affect_pointers"`
	JSON           string `json:"json"`
	Action         Action `json:"action"`
}

func Create(ctx context.Context, domain *cfg.Domain, typ dns.Type, name, value string) error {
	_, err := send(ctx, domain, dnsControlRequest{
		Domain:         domain.Domain, // see #3
		Type:           typ.String(),
		Name:           name,
		Value:          value,
		TTL:            "1800",
		AffectPointers: "no",
		JSON:           "yes",
		Action:         ActionAdd,
	})
	return err
}

// Delete removes a DNS record. DirectAdmin identifies which record to
// remove via a type-specific indexed field (e.g. "txtrecs0" for TXT,
// "arecs0" for A) whose value is the same "name=<name>&value=<value>"
// string List reports back as each record's "combined" field - not by
// separate name/value fields the way Create's "add" action works.
func Delete(ctx context.Context, domain *cfg.Domain, typ dns.Type, name, value string) error {
	typeStr := typ.String()
	recsKey := strings.ToLower(typeStr) + "recs0"

	_, err := send(ctx, domain, map[string]any{
		"domain": domain.Domain,
		"json":   "yes",
		"action": ActionSelect,
		"type":   typeStr,
		recsKey:  fmt.Sprintf("name=%s&value=%s", name, value),
	})
	return err
}

// Record is a single DNS record as returned by DirectAdmin's DNS listing.
type Record struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	TTL   string `json:"ttl"`
}

type listRequest struct {
	Domain string `json:"domain"`
	JSON   string `json:"json"`
}

type listResponse struct {
	Records []Record `json:"records"`
}

// List returns all DNS records currently configured for domain.
func List(ctx context.Context, domain *cfg.Domain) ([]Record, error) {
	body, err := send(ctx, domain, listRequest{
		Domain: domain.Domain,
		JSON:   "yes",
	})
	if err != nil {
		return nil, err
	}

	var resp listResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse records response: %v", err)
	}

	return resp.Records, nil
}

func send(ctx context.Context, domain *cfg.Domain, payload any) ([]byte, error) {
	uri, err := url.Parse(domain.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %v", err)
	}

	uri = uri.JoinPath(CMDDNSControl)

	q := url.Values{}
	q.Add("json", "yes")
	uri.RawQuery = q.Encode()

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal body: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to construct req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(domain.Username, domain.Password)

	client := http.Client{
		Transport: http.DefaultTransport,
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform http request: %v", err)
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got unexpected status code: %d, body: %s", res.StatusCode, respBody)
	}

	return respBody, nil
}
