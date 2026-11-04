// Package porkbun implements a cert-manager ACME DNS01 webhook solver that
// manages challenge TXT records through the Porkbun API.
package porkbun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	acme "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	pb "github.com/nrdcg/porkbun"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	// SolverName is the name referenced by issuers in webhook.solverName.
	SolverName = "porkbun"

	// recordTTL is the TTL of challenge records. Porkbun silently raises
	// anything lower to 600, so asking for it explicitly keeps state honest.
	recordTTL = "600"

	// requestTimeout bounds one Present or CleanUp call, Kubernetes and
	// Porkbun requests included.
	requestTimeout = 60 * time.Second
)

// Config is the solver configuration, given in the issuer's
// webhook.config stanza.
type Config struct {
	APIKeySecretRef    corev1.SecretKeySelector `json:"apiKeySecretRef"`
	SecretKeySecretRef corev1.SecretKeySelector `json:"secretKeySecretRef"`
}

// Solver is the Porkbun DNS01 solver.
type Solver struct {
	kube       kubernetes.Interface
	baseURL    *url.URL
	httpClient *http.Client
}

var _ webhook.Solver = (*Solver)(nil)

// Option configures a Solver.
type Option func(*Solver)

// WithBaseURL points the solver at a different Porkbun API endpoint.
func WithBaseURL(u *url.URL) Option {
	return func(s *Solver) { s.baseURL = u }
}

// WithHTTPClient sets the HTTP client used for Porkbun API calls.
func WithHTTPClient(c *http.Client) Option {
	return func(s *Solver) { s.httpClient = c }
}

// New returns a Solver. Initialize must be called before use.
func New(opts ...Option) *Solver {
	s := &Solver{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name implements webhook.Solver.
func (s *Solver) Name() string {
	return SolverName
}

// Initialize implements webhook.Solver.
func (s *Solver) Initialize(kubeClientConfig *rest.Config, _ <-chan struct{}) error {
	kube, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}
	s.kube = kube
	return nil
}

// Present implements webhook.Solver. It creates the challenge TXT record
// unless an identical one already exists.
func (s *Solver) Present(ch *acme.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	target, err := resolveTarget(ch)
	if err != nil {
		return err
	}
	logger := klog.LoggerWithValues(klog.Background(), "fqdn", target.fqdn, "domain", target.domain)

	client, err := s.client(ctx, ch)
	if err != nil {
		return err
	}

	records, err := client.RetrieveRecords(ctx, target.domain)
	if err != nil {
		return fmt.Errorf("retrieving records of %q: %w", target.domain, err)
	}
	if existing := matchingRecords(records, target.fqdn, ch.Key); len(existing) > 0 {
		logger.Info("Challenge record already present", "id", existing[0].ID)
		return nil
	}

	id, err := client.CreateRecord(ctx, target.domain, pb.Record{
		Name:    target.subdomain,
		Type:    "TXT",
		Content: ch.Key,
		TTL:     recordTTL,
	})
	if err != nil {
		return fmt.Errorf("creating TXT record %q: %w", target.fqdn, err)
	}

	logger.Info("Created challenge record", "id", id)
	return nil
}

// CleanUp implements webhook.Solver. It deletes every TXT record carrying
// this challenge's key, leaving records for other challenges alone.
func (s *Solver) CleanUp(ch *acme.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	target, err := resolveTarget(ch)
	if err != nil {
		return err
	}
	logger := klog.LoggerWithValues(klog.Background(), "fqdn", target.fqdn, "domain", target.domain)

	client, err := s.client(ctx, ch)
	if err != nil {
		return err
	}

	records, err := client.RetrieveRecords(ctx, target.domain)
	if err != nil {
		return fmt.Errorf("retrieving records of %q: %w", target.domain, err)
	}

	matches := matchingRecords(records, target.fqdn, ch.Key)
	if len(matches) == 0 {
		logger.Info("No challenge record to delete")
		return nil
	}

	var errs []error
	for _, rec := range matches {
		id, err := strconv.Atoi(rec.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("record %q has a malformed ID: %w", rec.ID, err))
			continue
		}
		if err := client.DeleteRecord(ctx, target.domain, id); err != nil {
			errs = append(errs, fmt.Errorf("deleting record %d: %w", id, err))
			continue
		}
		logger.Info("Deleted challenge record", "id", id)
	}
	return errors.Join(errs...)
}

// target is where a challenge record lives, in the terms of the Porkbun API.
type target struct {
	// domain is the Porkbun domain (zone) without a trailing dot.
	domain string
	// subdomain is the record name relative to domain, empty for the apex.
	subdomain string
	// fqdn is the full record name without a trailing dot, as Porkbun
	// returns it when listing records.
	fqdn string
}

func resolveTarget(ch *acme.ChallengeRequest) (target, error) {
	zone := normalizeName(ch.ResolvedZone)
	fqdn := normalizeName(ch.ResolvedFQDN)
	if zone == "" {
		return target{}, errors.New("challenge has no resolved zone")
	}

	switch {
	case fqdn == zone:
		return target{domain: zone, fqdn: fqdn}, nil
	case strings.HasSuffix(fqdn, "."+zone):
		return target{domain: zone, subdomain: strings.TrimSuffix(fqdn, "."+zone), fqdn: fqdn}, nil
	default:
		return target{}, fmt.Errorf("record %q is not inside zone %q", ch.ResolvedFQDN, ch.ResolvedZone)
	}
}

// normalizeName lowercases a DNS name and strips the trailing root dot.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// matchingRecords returns the TXT records named fqdn whose content is key.
func matchingRecords(records []pb.Record, fqdn, key string) []pb.Record {
	var out []pb.Record
	for _, rec := range records {
		if !strings.EqualFold(rec.Type, "TXT") || normalizeName(rec.Name) != fqdn {
			continue
		}
		// Tolerate the content coming back quoted, as zone-file style
		// tools sometimes store it.
		if rec.Content == key || strings.Trim(rec.Content, `"`) == key {
			out = append(out, rec)
		}
	}
	return out
}

func (s *Solver) client(ctx context.Context, ch *acme.ChallengeRequest) (*pb.Client, error) {
	cfg, err := loadConfig(ch)
	if err != nil {
		return nil, err
	}

	apiKey, err := s.secretValue(ctx, ch.ResourceNamespace, cfg.APIKeySecretRef, "apiKeySecretRef")
	if err != nil {
		return nil, err
	}
	secretKey, err := s.secretValue(ctx, ch.ResourceNamespace, cfg.SecretKeySecretRef, "secretKeySecretRef")
	if err != nil {
		return nil, err
	}

	client := pb.New(secretKey, apiKey)
	if s.baseURL != nil {
		client.BaseURL = s.baseURL
	}
	if s.httpClient != nil {
		client.HTTPClient = s.httpClient
	}
	return client, nil
}

func loadConfig(ch *acme.ChallengeRequest) (Config, error) {
	var cfg Config
	if ch.Config == nil {
		return cfg, errors.New("webhook config is missing: apiKeySecretRef and secretKeySecretRef are required")
	}
	if err := json.Unmarshal(ch.Config.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("decoding webhook config: %w", err)
	}
	return cfg, nil
}

func (s *Solver) secretValue(ctx context.Context, namespace string, ref corev1.SecretKeySelector, field string) (string, error) {
	if ref.Name == "" || ref.Key == "" {
		return "", fmt.Errorf("%s: name and key are required", field)
	}
	if s.kube == nil {
		return "", errors.New("solver is not initialized")
	}

	secret, err := s.kube.CoreV1().Secrets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("%s: getting secret %s/%s: %w", field, namespace, ref.Name, err)
	}

	raw, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("%s: secret %s/%s has no key %q", field, namespace, ref.Name, ref.Key)
	}
	// A trailing newline from `echo ... | kubectl create secret` is a common
	// mistake that Porkbun answers with an opaque authentication error.
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", fmt.Errorf("%s: key %q of secret %s/%s is empty", field, ref.Key, namespace, ref.Name)
	}
	return value, nil
}
