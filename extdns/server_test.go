package extdns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
	"git.dataverket.org/tools/nordhost-integrator/da"
)

func toRecords(raw []map[string]string) []da.Record {
	records := make([]da.Record, len(raw))
	for i, r := range raw {
		records[i] = da.Record{Type: r["type"], Name: r["name"], Value: r["value"], TTL: r["ttl"]}
	}
	return records
}

func jsonReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}

// fakeDirectAdmin emulates DirectAdmin's CMD_DNS_CONTROL endpoint against
// an in-memory record set, so extdns can be tested without a live account.
func fakeDirectAdmin(t *testing.T, records *[]map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("fake DirectAdmin: bad request body: %v", err)
		}

		switch req["action"] {
		case "add":
			*records = append(*records, map[string]string{
				"type":  req["type"].(string),
				"name":  req["name"].(string),
				"value": req["value"].(string),
				"ttl":   "1800",
			})
		case "select":
			// Real DirectAdmin identifies the record via a type-specific
			// indexed field (e.g. "txtrecs0") whose value is
			// "name=X&value=Y" - not plain name/value fields. See da.Delete.
			typeStr, _ := req["type"].(string)
			recsKey := strings.ToLower(typeStr) + "recs0"
			combined, _ := req[recsKey].(string)

			out := (*records)[:0]
			for _, rec := range *records {
				recCombined := fmt.Sprintf("name=%s&value=%s", rec["name"], rec["value"])
				if rec["type"] == typeStr && recCombined == combined {
					continue
				}
				out = append(out, rec)
			}
			*records = out
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"records": *records})
	}))
}

func testConfig(t *testing.T, records *[]map[string]string) *cfg.Config {
	t.Helper()
	da := fakeDirectAdmin(t, records)
	t.Cleanup(da.Close)
	return &cfg.Config{
		Domains: []cfg.Domain{
			{Domain: "example.com", Host: da.URL, Username: "u", Password: "p"},
		},
	}
}

func TestFqdn(t *testing.T) {
	cases := []struct{ name, domain, want string }{
		{"example.com.", "example.com", "example.com"},
		{"www", "example.com", "www.example.com"},
		{"_acme-challenge.example.com.", "example.com", "_acme-challenge.example.com"},
	}
	for _, c := range cases {
		if got := fqdn(c.name, c.domain); got != c.want {
			t.Errorf("fqdn(%q, %q) = %q, want %q", c.name, c.domain, got, c.want)
		}
	}
}

func TestRecordsToEndpoints_GroupsRoundRobin(t *testing.T) {
	records := []map[string]string{
		{"type": "A", "name": "example.com.", "value": "1.1.1.1", "ttl": "1800"},
		{"type": "A", "name": "example.com.", "value": "2.2.2.2", "ttl": "1800"},
		{"type": "CNAME", "name": "www", "value": "example.com.", "ttl": "1800"},
	}
	daRecords := toRecords(records)

	endpoints := recordsToEndpoints("example.com", daRecords)
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d: %+v", len(endpoints), endpoints)
	}

	apex := endpoints[0]
	if apex.DNSName != "example.com" || apex.RecordType != "A" || len(apex.Targets) != 2 {
		t.Errorf("unexpected apex endpoint: %+v", apex)
	}

	www := endpoints[1]
	if www.DNSName != "www.example.com" || www.RecordType != "CNAME" {
		t.Errorf("unexpected www endpoint: %+v", www)
	}
}

func TestHandleNegotiate(t *testing.T) {
	records := []map[string]string{}
	s := New(testConfig(t, &records))
	srv := httptest.NewServer(s.RecordsHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var got struct {
		Filters []string `json:"filters"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Filters) != 1 || got.Filters[0] != "example.com" {
		t.Errorf("unexpected filters: %+v", got.Filters)
	}
	if ct := res.Header.Get("Content-Type"); ct != mediaType {
		t.Errorf("unexpected Content-Type: %q", ct)
	}
}

func TestHandleGetRecords(t *testing.T) {
	records := []map[string]string{
		{"type": "A", "name": "example.com.", "value": "1.1.1.1", "ttl": "1800"},
	}
	s := New(testConfig(t, &records))
	srv := httptest.NewServer(s.RecordsHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/records")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var endpoints []Endpoint
	if err := json.NewDecoder(res.Body).Decode(&endpoints); err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || endpoints[0].DNSName != "example.com" {
		t.Errorf("unexpected endpoints: %+v", endpoints)
	}
}

func TestHandleApplyChanges_Create(t *testing.T) {
	records := []map[string]string{}
	s := New(testConfig(t, &records))
	srv := httptest.NewServer(s.RecordsHandler())
	defer srv.Close()

	changes := Changes{
		Create: []Endpoint{
			{DNSName: "www.example.com", RecordType: "A", Targets: []string{"1.2.3.4"}},
		},
	}
	body, _ := json.Marshal(changes)

	res, err := http.Post(srv.URL+"/records", mediaType, jsonReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}
	if len(records) != 1 || records[0]["name"] != "www" || records[0]["value"] != "1.2.3.4" {
		t.Errorf("unexpected records after create: %+v", records)
	}
}

func TestHandleApplyChanges_Delete(t *testing.T) {
	records := []map[string]string{
		{"type": "A", "name": "www", "value": "1.2.3.4", "ttl": "1800"},
	}
	s := New(testConfig(t, &records))
	srv := httptest.NewServer(s.RecordsHandler())
	defer srv.Close()

	changes := Changes{
		Delete: []Endpoint{
			{DNSName: "www.example.com", RecordType: "A", Targets: []string{"1.2.3.4"}},
		},
	}
	body, _ := json.Marshal(changes)

	res, err := http.Post(srv.URL+"/records", mediaType, jsonReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}
	if len(records) != 0 {
		t.Errorf("expected record to be deleted, got: %+v", records)
	}
}

func TestHandleApplyChanges_UpdateOnlyTouchesDiff(t *testing.T) {
	records := []map[string]string{
		{"type": "A", "name": "www", "value": "1.1.1.1", "ttl": "1800"},
		{"type": "A", "name": "www", "value": "2.2.2.2", "ttl": "1800"},
	}
	s := New(testConfig(t, &records))
	srv := httptest.NewServer(s.RecordsHandler())
	defer srv.Close()

	// Keep 1.1.1.1, drop 2.2.2.2, add 3.3.3.3.
	changes := Changes{
		UpdateOld: []Endpoint{
			{DNSName: "www.example.com", RecordType: "A", Targets: []string{"1.1.1.1", "2.2.2.2"}},
		},
		UpdateNew: []Endpoint{
			{DNSName: "www.example.com", RecordType: "A", Targets: []string{"1.1.1.1", "3.3.3.3"}},
		},
	}
	body, _ := json.Marshal(changes)

	res, err := http.Post(srv.URL+"/records", mediaType, jsonReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}

	values := map[string]bool{}
	for _, r := range records {
		values[r["value"]] = true
	}
	if !values["1.1.1.1"] || values["2.2.2.2"] || !values["3.3.3.3"] || len(records) != 2 {
		t.Errorf("unexpected records after update: %+v", records)
	}
}

func TestHandleApplyChanges_UnknownDomain(t *testing.T) {
	records := []map[string]string{}
	s := New(testConfig(t, &records))
	srv := httptest.NewServer(s.RecordsHandler())
	defer srv.Close()

	changes := Changes{
		Create: []Endpoint{
			{DNSName: "www.not-configured.com", RecordType: "A", Targets: []string{"1.2.3.4"}},
		},
	}
	body, _ := json.Marshal(changes)

	res, err := http.Post(srv.URL+"/records", mediaType, jsonReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestHandleAdjustEndpoints_PassThrough(t *testing.T) {
	records := []map[string]string{}
	s := New(testConfig(t, &records))
	srv := httptest.NewServer(s.RecordsHandler())
	defer srv.Close()

	in := []Endpoint{{DNSName: "www.example.com", RecordType: "A", Targets: []string{"1.2.3.4"}}}
	body, _ := json.Marshal(in)

	res, err := http.Post(srv.URL+"/adjustendpoints", mediaType, jsonReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var out []Endpoint
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].DNSName != "www.example.com" {
		t.Errorf("unexpected pass-through result: %+v", out)
	}
}

func TestHealthHandler(t *testing.T) {
	srv := httptest.NewServer(HealthHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}
