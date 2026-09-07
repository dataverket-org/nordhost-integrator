package nordhost

import (
	"context"
	"errors"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
	"git.dataverket.org/tools/nordhost-integrator/da"

	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	"github.com/miekg/dns"
)

const GroupName = "nordhost-integrator"

var ErrDomainNotConfigured = errors.New("the requested domain is not configured.")

type Server struct {
	c *cfg.Config
}

func New(config *cfg.Config) (*Server, error) {
	s := new(Server)
	s.c = config

	return s, nil
}

func (s *Server) Run() {
	cmd.RunWebhookServer(GroupName, s)
}

func (s *Server) Name() string {
	return "nordhost-solver"
}

func (s *Server) Present(ch *v1alpha1.ChallengeRequest) error {
	dom := s.c.FindDomain(ch.ResolvedZone)
	if dom == nil {
		return ErrDomainNotConfigured
	}

	name := cfg.RecordName(ch.ResolvedFQDN, dom.Domain)

	return da.Create(context.Background(), dom, dns.Type(dns.TypeTXT), name, ch.Key)
}

func (s *Server) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	dom := s.c.FindDomain(ch.ResolvedZone)
	if dom == nil {
		return ErrDomainNotConfigured
	}

	name := cfg.RecordName(ch.ResolvedFQDN, dom.Domain)

	return da.Delete(context.Background(), dom, dns.Type(dns.TypeTXT), name, ch.Key)
}

func (s *Server) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	return nil
}
