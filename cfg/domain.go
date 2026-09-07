package cfg

import "strings"

type Domain struct {
	Domain   string `json:"domain"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// RecordName converts a fully-qualified DNS name (e.g.
// "_acme-challenge.example.com.") into the name relative to domain (e.g.
// "_acme-challenge") that DirectAdmin's DNS API expects.
func RecordName(fqdn, domain string) string {
	name := strings.TrimSuffix(fqdn, ".")
	return strings.TrimSuffix(name, "."+domain)
}
