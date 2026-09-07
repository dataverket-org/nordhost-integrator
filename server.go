package nordhost

import (
	"context"

	"git.dataverket.org/tools/nordhost-integrator/cfg"
	"git.dataverket.org/tools/nordhost-integrator/da"
	"github.com/miekg/dns"

	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
)

const GroupName = "nordhost-integrator"

type Server struct {
	c *cfg.Config
}

func New(config *cfg.Config) (*Server, error) {
	s := new(Server)
	s.c = config

	return s, da.Delete(context.Background(), &s.c.Domains[0], dns.Type(dns.TypeA), "bob", "8.8.8.8")
}

func (s *Server) Run() {
	cmd.RunWebhookServer(GroupName, s)
}

func (s *Server) Name() string {
	return "nordhost-solver"
}

func (s *Server) Present(ch *v1alpha1.ChallengeRequest) error {
	return nil
}

func (s *Server) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	return nil
}

func (s *Server) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	return nil
}
