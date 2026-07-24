package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type Resolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

type Policy struct {
	resolver Resolver
	allowed  []netip.Prefix
	dialer   net.Dialer
}

func NewPolicy(resolver Resolver, allowed []netip.Prefix) *Policy {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Policy{
		resolver: resolver,
		allowed:  append([]netip.Prefix(nil), allowed...),
		dialer: net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
}

func ParseAllowedPrefixes(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parse allowed CIDR %q: %w", strings.TrimSpace(part), err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func (p *Policy) ValidateHTTPSURL(ctx context.Context, raw string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("invalid target URL")
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("target must be an HTTPS URL without credentials")
	}
	if err := p.ValidateHost(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

func (p *Policy) Validate(ctx context.Context, raw string) error {
	_, err := p.ValidateHTTPSURL(ctx, raw)
	return err
}

func (p *Policy) ValidateHost(ctx context.Context, host string) error {
	addresses, err := p.lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve target host: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("target host resolved to no addresses")
	}
	for _, address := range addresses {
		if !p.isAllowed(address) {
			return errors.New("target address is blocked by outbound policy")
		}
	}
	return nil
}

func (p *Policy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid target address")
	}
	addresses, err := p.lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve target host: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("target host resolved to no addresses")
	}
	for _, candidate := range addresses {
		if !p.isAllowed(candidate) {
			return nil, errors.New("target address is blocked by outbound policy")
		}
	}

	var lastErr error
	for _, candidate := range addresses {
		connection, dialErr := p.dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, fmt.Errorf("connect to validated target: %w", lastErr)
}

func (p *Policy) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if parsed, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return []netip.Addr{parsed.Unmap()}, nil
	}
	resolved, err := p.resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, address := range resolved {
		parsed, ok := netip.AddrFromSlice(address)
		if !ok {
			return nil, errors.New("resolver returned an invalid address")
		}
		addresses = append(addresses, parsed.Unmap())
	}
	return addresses, nil
}

func (p *Policy) isAllowed(address netip.Addr) bool {
	for _, prefix := range p.allowed {
		if prefix.Contains(address) {
			return true
		}
	}
	return address.IsValid() &&
		address.IsGlobalUnicast() &&
		!address.IsPrivate() &&
		!address.IsLoopback() &&
		!address.IsLinkLocalUnicast() &&
		!address.IsLinkLocalMulticast() &&
		!address.IsMulticast() &&
		!address.IsUnspecified()
}
