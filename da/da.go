package da

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

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

	ActionAdd    Action = "add"
	ActionDelete Action = "delete"
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
	return send(ctx, domain, dnsControlRequest{
		Domain:         domain.Domain, // see #3
		Type:           typ.String(),
		Name:           name,
		Value:          value,
		TTL:            "1800",
		AffectPointers: "no",
		JSON:           "yes",
		Action:         ActionAdd,
	})
}

func Delete(ctx context.Context, domain *cfg.Domain, typ dns.Type, name, value string) error {
	return send(ctx, domain, dnsControlRequest{
		Domain:         domain.Domain,
		Type:           typ.String(),
		Name:           name,
		Value:          value,
		TTL:            "1800",
		AffectPointers: "no",
		JSON:           "yes",
		Action:         ActionDelete,
	})
}

func send(ctx context.Context, domain *cfg.Domain, payload dnsControlRequest) error {
	uri, err := url.Parse(domain.Host)
	if err != nil {
		return fmt.Errorf("failed to parse url: %v", err)
	}

	uri = uri.JoinPath(CMDDNSControl)

	q := url.Values{}
	q.Add("json", "yes")
	uri.RawQuery = q.Encode()

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, uri.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to construct req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(domain.Username, domain.Password)

	client := http.Client{
		Transport: http.DefaultTransport,
	}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform http request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("got unexpected status code: %d, body: %s", res.StatusCode, respBody)
	}

	return nil
}
