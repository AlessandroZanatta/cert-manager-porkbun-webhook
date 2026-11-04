package porkbun

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	acme "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	pb "github.com/nrdcg/porkbun"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/AlessandroZanatta/cert-manager-porkbun-webhook/internal/porkbuntest"
)

const (
	testNamespace = "cert-manager"
	testAPIKey    = "pk1_test"
	testSecretKey = "sk1_test"
	testZone      = "example.com"
)

func testSecret(data map[string]string) *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "porkbun", Namespace: testNamespace},
		Data:       map[string][]byte{},
	}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func defaultSecret() *corev1.Secret {
	return testSecret(map[string]string{"api-key": testAPIKey, "secret-key": testSecretKey})
}

func newTestSolver(t *testing.T, objs ...runtime.Object) (*Solver, *porkbuntest.Server) {
	t.Helper()
	api := porkbuntest.New(t, testZone)
	api.RequireCredentials(testAPIKey, testSecretKey)
	s := New(WithBaseURL(api.URL()))
	s.kube = fake.NewClientset(objs...)
	return s, api
}

func rawConfig(t *testing.T, cfg any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &apiextensionsv1.JSON{Raw: raw}
}

func defaultConfig(t *testing.T) *apiextensionsv1.JSON {
	return rawConfig(t, Config{
		APIKeySecretRef:    corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "porkbun"}, Key: "api-key"},
		SecretKeySecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "porkbun"}, Key: "secret-key"},
	})
}

func challenge(t *testing.T, fqdn, zone, key string) *acme.ChallengeRequest {
	return &acme.ChallengeRequest{
		ResourceNamespace: testNamespace,
		ResolvedFQDN:      fqdn,
		ResolvedZone:      zone,
		Key:               key,
		Config:            defaultConfig(t),
	}
}

// summary renders records as sorted "name type content ttl" lines.
func summary(recs []pb.Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, strings.Join([]string{r.Name, r.Type, r.Content, r.TTL}, " "))
	}
	slices.Sort(out)
	return out
}

func actions(calls []porkbuntest.Call) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Action+" "+c.Zone)
	}
	return out
}

func assertEqual(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got: %q\nwant: %q", what, got, want)
	}
}

func TestName(t *testing.T) {
	if got := New().Name(); got != "porkbun" {
		t.Errorf("Name() = %q, want porkbun", got)
	}
}

func TestPresent(t *testing.T) {
	tests := []struct {
		name        string
		fqdn, zone  string
		seed        []pb.Record
		wantRecords []string
		wantCalls   []string
	}{
		{
			name:        "creates record",
			fqdn:        "_acme-challenge.example.com.",
			zone:        "example.com.",
			wantRecords: []string{"_acme-challenge.example.com TXT key1 600"},
			wantCalls:   []string{"retrieve example.com", "create example.com"},
		},
		{
			name:        "nested subdomain",
			fqdn:        "_acme-challenge.a.b.example.com.",
			zone:        "example.com.",
			wantRecords: []string{"_acme-challenge.a.b.example.com TXT key1 600"},
			wantCalls:   []string{"retrieve example.com", "create example.com"},
		},
		{
			name:        "mixed case names",
			fqdn:        "_ACME-Challenge.Example.COM.",
			zone:        "Example.COM.",
			wantRecords: []string{"_acme-challenge.example.com TXT key1 600"},
			wantCalls:   []string{"retrieve example.com", "create example.com"},
		},
		{
			name:        "fqdn equals zone creates apex record",
			fqdn:        "example.com.",
			zone:        "example.com.",
			wantRecords: []string{"example.com TXT key1 600"},
			wantCalls:   []string{"retrieve example.com", "create example.com"},
		},
		{
			name: "already present is a no-op",
			fqdn: "_acme-challenge.example.com.",
			zone: "example.com.",
			seed: []pb.Record{{Name: "_acme-challenge", Type: "TXT", Content: "key1"}},
			wantRecords: []string{
				"_acme-challenge.example.com TXT key1 600",
			},
			wantCalls: []string{"retrieve example.com"},
		},
		{
			name: "keeps other challenges for the same name",
			fqdn: "_acme-challenge.example.com.",
			zone: "example.com.",
			seed: []pb.Record{
				{Name: "_acme-challenge", Type: "TXT", Content: "other"},
				{Name: "www", Type: "A", Content: "192.0.2.1"},
			},
			wantRecords: []string{
				"_acme-challenge.example.com TXT key1 600",
				"_acme-challenge.example.com TXT other 600",
				"www.example.com A 192.0.2.1 600",
			},
			wantCalls: []string{"retrieve example.com", "create example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, api := newTestSolver(t, defaultSecret())
			api.Seed(testZone, tt.seed...)

			if err := s.Present(challenge(t, tt.fqdn, tt.zone, "key1")); err != nil {
				t.Fatalf("Present: %v", err)
			}
			assertEqual(t, "records", summary(api.Records(testZone)), tt.wantRecords)
			assertEqual(t, "calls", actions(api.Calls()), tt.wantCalls)
		})
	}
}

func TestPresentIsIdempotent(t *testing.T) {
	s, api := newTestSolver(t, defaultSecret())
	ch := challenge(t, "_acme-challenge.example.com.", "example.com.", "key1")
	for range 3 {
		if err := s.Present(ch); err != nil {
			t.Fatalf("Present: %v", err)
		}
	}
	assertEqual(t, "records", summary(api.Records(testZone)), []string{"_acme-challenge.example.com TXT key1 600"})
}

func TestCleanUp(t *testing.T) {
	tests := []struct {
		name        string
		seed        []pb.Record
		wantRecords []string
		wantCalls   []string
	}{
		{
			name:      "nothing to delete",
			seed:      []pb.Record{{Name: "www", Type: "A", Content: "192.0.2.1"}},
			wantCalls: []string{"retrieve example.com"},
			wantRecords: []string{
				"www.example.com A 192.0.2.1 600",
			},
		},
		{
			name: "deletes only the matching key",
			seed: []pb.Record{
				{Name: "_acme-challenge", Type: "TXT", Content: "key1"},
				{Name: "_acme-challenge", Type: "TXT", Content: "other"},
				{Name: "_acme-challenge.sub", Type: "TXT", Content: "key1"},
				{Name: "", Type: "TXT", Content: "key1"},
			},
			wantRecords: []string{
				"_acme-challenge.example.com TXT other 600",
				"_acme-challenge.sub.example.com TXT key1 600",
				"example.com TXT key1 600",
			},
			wantCalls: []string{"retrieve example.com", "delete example.com"},
		},
		{
			name: "deletes duplicates",
			seed: []pb.Record{
				{Name: "_acme-challenge", Type: "TXT", Content: "key1"},
				{Name: "_acme-challenge", Type: "TXT", Content: "key1"},
			},
			wantCalls: []string{"retrieve example.com", "delete example.com", "delete example.com"},
		},
		{
			name: "deletes quoted content",
			seed: []pb.Record{
				{Name: "_acme-challenge", Type: "TXT", Content: `"key1"`},
			},
			wantCalls: []string{"retrieve example.com", "delete example.com"},
		},
		{
			name: "ignores other record types",
			seed: []pb.Record{
				{Name: "_acme-challenge", Type: "CNAME", Content: "key1"},
			},
			wantRecords: []string{"_acme-challenge.example.com CNAME key1 600"},
			wantCalls:   []string{"retrieve example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, api := newTestSolver(t, defaultSecret())
			api.Seed(testZone, tt.seed...)

			if err := s.CleanUp(challenge(t, "_acme-challenge.example.com.", "example.com.", "key1")); err != nil {
				t.Fatalf("CleanUp: %v", err)
			}
			if tt.wantRecords == nil {
				tt.wantRecords = []string{}
			}
			assertEqual(t, "records", summary(api.Records(testZone)), tt.wantRecords)
			assertEqual(t, "calls", actions(api.Calls()), tt.wantCalls)
		})
	}
}

func TestPresentThenCleanUp(t *testing.T) {
	s, api := newTestSolver(t, defaultSecret())
	api.Seed(testZone, pb.Record{Name: "www", Type: "A", Content: "192.0.2.1"})
	ch := challenge(t, "_acme-challenge.www.example.com.", "example.com.", "key1")

	if err := s.Present(ch); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if err := s.CleanUp(ch); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	assertEqual(t, "records", summary(api.Records(testZone)), []string{"www.example.com A 192.0.2.1 600"})
}

func TestSendsCredentials(t *testing.T) {
	// The fake rejects swapped or untrimmed keys, so a successful call proves
	// the API and secret key reach Porkbun in the right fields.
	s, _ := newTestSolver(t, testSecret(map[string]string{
		"api-key":    testAPIKey + "\n",
		"secret-key": "  " + testSecretKey,
	}))
	if err := s.Present(challenge(t, "_acme-challenge.example.com.", "example.com.", "key1")); err != nil {
		t.Fatalf("Present: %v", err)
	}
}

func TestErrors(t *testing.T) {
	selector := func(name, key string) corev1.SecretKeySelector {
		return corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key}
	}

	tests := []struct {
		name    string
		secret  *corev1.Secret
		mutate  func(*acme.ChallengeRequest)
		fail    string
		wantErr string
	}{
		{
			name:    "missing config",
			mutate:  func(ch *acme.ChallengeRequest) { ch.Config = nil },
			wantErr: "webhook config is missing",
		},
		{
			name: "malformed config",
			mutate: func(ch *acme.ChallengeRequest) {
				ch.Config = &apiextensionsv1.JSON{Raw: []byte(`{"apiKeySecretRef": 1}`)}
			},
			wantErr: "decoding webhook config",
		},
		{
			name: "empty secret ref",
			mutate: func(ch *acme.ChallengeRequest) {
				ch.Config = rawConfig(t, Config{SecretKeySecretRef: selector("porkbun", "secret-key")})
			},
			wantErr: "apiKeySecretRef: name and key are required",
		},
		{
			name: "secret not found",
			mutate: func(ch *acme.ChallengeRequest) {
				ch.Config = rawConfig(t, Config{
					APIKeySecretRef:    selector("missing", "api-key"),
					SecretKeySecretRef: selector("missing", "secret-key"),
				})
			},
			wantErr: `getting secret cert-manager/missing`,
		},
		{
			name:    "secret key absent",
			secret:  testSecret(map[string]string{"api-key": testAPIKey}),
			wantErr: `secretKeySecretRef: secret cert-manager/porkbun has no key "secret-key"`,
		},
		{
			name:    "secret value empty",
			secret:  testSecret(map[string]string{"api-key": " \n", "secret-key": testSecretKey}),
			wantErr: `key "api-key" of secret cert-manager/porkbun is empty`,
		},
		{
			name:    "wrong credentials",
			secret:  testSecret(map[string]string{"api-key": "wrong", "secret-key": testSecretKey}),
			wantErr: "Invalid API key",
		},
		{
			name:    "fqdn outside zone",
			mutate:  func(ch *acme.ChallengeRequest) { ch.ResolvedFQDN = "_acme-challenge.example.org." },
			wantErr: "is not inside zone",
		},
		{
			name:    "suffix but not subdomain",
			mutate:  func(ch *acme.ChallengeRequest) { ch.ResolvedFQDN = "_acme-challenge.notexample.com." },
			wantErr: "is not inside zone",
		},
		{
			name:    "no zone",
			mutate:  func(ch *acme.ChallengeRequest) { ch.ResolvedZone = "" },
			wantErr: "no resolved zone",
		},
		{
			name: "unknown domain",
			mutate: func(ch *acme.ChallengeRequest) {
				ch.ResolvedFQDN, ch.ResolvedZone = "_acme-challenge.other.com.", "other.com."
			},
			wantErr: "Invalid domain",
		},
		{
			name:    "retrieve fails",
			fail:    "retrieve",
			wantErr: "retrieving records",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := tt.secret
			if secret == nil {
				secret = defaultSecret()
			}
			for _, op := range []string{"Present", "CleanUp"} {
				s, api := newTestSolver(t, secret)
				if tt.fail != "" {
					api.Fail(tt.fail, "boom")
				}
				ch := challenge(t, "_acme-challenge.example.com.", "example.com.", "key1")
				if tt.mutate != nil {
					tt.mutate(ch)
				}

				var err error
				if op == "Present" {
					err = s.Present(ch)
				} else {
					err = s.CleanUp(ch)
				}
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("%s: error = %v, want containing %q", op, err, tt.wantErr)
				}
			}
		})
	}
}

func TestCreateFails(t *testing.T) {
	s, api := newTestSolver(t, defaultSecret())
	api.Fail("create", "boom")
	err := s.Present(challenge(t, "_acme-challenge.example.com.", "example.com.", "key1"))
	if err == nil || !strings.Contains(err.Error(), "creating TXT record") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Present error = %v", err)
	}
}

func TestDeleteFailsReportsEveryRecord(t *testing.T) {
	s, api := newTestSolver(t, defaultSecret())
	api.Seed(testZone,
		pb.Record{Name: "_acme-challenge", Type: "TXT", Content: "key1"},
		pb.Record{Name: "_acme-challenge", Type: "TXT", Content: "key1"},
	)
	api.Fail("delete", "boom")
	err := s.CleanUp(challenge(t, "_acme-challenge.example.com.", "example.com.", "key1"))
	if err == nil {
		t.Fatal("CleanUp succeeded, want error")
	}
	if got := strings.Count(err.Error(), "boom"); got != 2 {
		t.Errorf("error mentions %d failures, want 2: %v", got, err)
	}
	assertEqual(t, "calls", actions(api.Calls()), []string{"retrieve example.com", "delete example.com", "delete example.com"})
}

func TestUninitialized(t *testing.T) {
	err := New().Present(challenge(t, "_acme-challenge.example.com.", "example.com.", "key1"))
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("Present error = %v", err)
	}
}
