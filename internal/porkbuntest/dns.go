package porkbuntest

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// ServeDNS starts an authoritative DNS server on 127.0.0.1 answering TXT
// queries from the fake's records, and returns its host:port. It lets
// cert-manager's conformance suite verify propagation without the internet.
// Changes made through the API are visible immediately.
func (s *Server) ServeDNS(t testing.TB) string {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening for DNS: %v", err)
	}
	started := make(chan struct{})
	srv := &dns.Server{
		PacketConn:        pc,
		Handler:           dns.HandlerFunc(s.answer),
		NotifyStartedFunc: func() { close(started) },
	}
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}

func (s *Server) answer(w dns.ResponseWriter, req *dns.Msg) {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Authoritative = true

	if len(req.Question) != 1 {
		resp.Rcode = dns.RcodeFormatError
		_ = w.WriteMsg(resp)
		return
	}
	q := req.Question[0]
	name := strings.ToLower(strings.TrimSuffix(q.Name, "."))

	s.mu.Lock()
	defer s.mu.Unlock()

	zone, ok := s.zoneOf(name)
	if !ok {
		resp.Rcode = dns.RcodeRefused
		_ = w.WriteMsg(resp)
		return
	}

	exists := false
	for _, rec := range s.zones[zone] {
		if !strings.EqualFold(rec.Name, name) {
			continue
		}
		exists = true
		if q.Qtype != dns.TypeTXT || !strings.EqualFold(rec.Type, "TXT") {
			continue
		}
		ttl, _ := strconv.Atoi(rec.TTL)
		resp.Answer = append(resp.Answer, &dns.TXT{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: uint32(ttl)}, //nolint:gosec // TTL is always small.
			Txt: []string{rec.Content},
		})
	}
	if !exists {
		resp.Rcode = dns.RcodeNameError
	}
	_ = w.WriteMsg(resp)
}

// zoneOf returns the served zone containing name. Callers must hold s.mu.
func (s *Server) zoneOf(name string) (string, bool) {
	for zone := range s.zones {
		if name == zone || strings.HasSuffix(name, "."+zone) {
			return zone, true
		}
	}
	return "", false
}
