//go:build conformance

package main

import (
	"testing"
	"time"

	acmetest "github.com/cert-manager/cert-manager/test/acme"

	"github.com/AlessandroZanatta/cert-manager-porkbun-webhook/internal/porkbuntest"
	"github.com/AlessandroZanatta/cert-manager-porkbun-webhook/porkbun"
)

// TestConformance runs cert-manager's DNS01 webhook conformance suite against
// a fake Porkbun API and a DNS server serving the fake's records, so it needs
// no Porkbun account or internet access.
//
// cert-manager's harness starts a kube-apiserver and etcd from envtest and
// panics at package init when it cannot find them, hence the build tag. Run it
// with `make test`, which provisions the binaries and sets TEST_ASSET_*.
func TestConformance(t *testing.T) {
	const zone = "example.com"
	api := porkbuntest.New(t, zone)
	// Must match testdata/porkbun-solver/porkbun-secret.yaml.
	api.RequireCredentials("pk1_conformance", "sk1_conformance")
	dnsAddr := api.ServeDNS(t)

	fixture := acmetest.NewFixture(porkbun.New(porkbun.WithBaseURL(api.URL())),
		acmetest.SetResolvedZone(zone+"."),
		acmetest.SetResolvedFQDN("_acme-challenge."+zone+"."),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath("testdata/porkbun-solver"),
		acmetest.SetDNSServer(dnsAddr),
		acmetest.SetUseAuthoritative(false),
		acmetest.SetStrict(true),
		acmetest.SetPollInterval(100*time.Millisecond),
		acmetest.SetPropagationLimit(10*time.Second),
	)
	fixture.RunConformance(t)

	if recs := api.Records(zone); len(recs) != 0 {
		t.Errorf("records left behind after conformance run: %+v", recs)
	}
}
