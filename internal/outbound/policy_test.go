package outbound

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

type fakeResolver map[string][]net.IP

func (r fakeResolver) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	addresses, ok := r[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

func TestPolicyValidateHTTPSURL(t *testing.T) {
	t.Parallel()

	policy := NewPolicy(fakeResolver{
		"public.example.com":  {net.ParseIP("203.0.113.10")},
		"private.example.com": {net.ParseIP("10.1.2.3")},
		"mixed.example.com":   {net.ParseIP("203.0.113.11"), net.ParseIP("127.0.0.1")},
	}, nil)

	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "public HTTPS", rawURL: "https://public.example.com:6443"},
		{name: "HTTP", rawURL: "http://public.example.com", wantErr: true},
		{name: "private", rawURL: "https://private.example.com", wantErr: true},
		{name: "mixed DNS", rawURL: "https://mixed.example.com", wantErr: true},
		{name: "userinfo", rawURL: "https://user:pass@public.example.com", wantErr: true},
		{name: "unknown host", rawURL: "https://unknown.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := policy.ValidateHTTPSURL(context.Background(), tt.rawURL)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateHTTPSURL() error = nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateHTTPSURL() error = %v", err)
			}
		})
	}
}

func TestPolicyAllowsConfiguredPrivateCIDR(t *testing.T) {
	t.Parallel()

	prefix := netip.MustParsePrefix("10.20.0.0/16")
	policy := NewPolicy(fakeResolver{
		"cluster.internal": {net.ParseIP("10.20.1.9")},
	}, []netip.Prefix{prefix})

	if _, err := policy.ValidateHTTPSURL(context.Background(), "https://cluster.internal:6443"); err != nil {
		t.Fatalf("ValidateHTTPSURL() error = %v", err)
	}
}

func TestParseAllowedPrefixes(t *testing.T) {
	t.Parallel()

	prefixes, err := ParseAllowedPrefixes("10.20.0.0/16, 192.168.8.10/32")
	if err != nil {
		t.Fatalf("ParseAllowedPrefixes() error = %v", err)
	}
	if len(prefixes) != 2 {
		t.Fatalf("len(prefixes) = %d, want 2", len(prefixes))
	}
	if _, err := ParseAllowedPrefixes("10.20.0.0/99"); err == nil {
		t.Fatal("ParseAllowedPrefixes() accepted invalid CIDR")
	}
}
