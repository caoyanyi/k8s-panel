package kubernetes

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	endpointCertificateMaxResponseBytes = 64 * 1024
	endpointCertificateExpiringWindow   = 30 * 24 * time.Hour
	endpointCertificateCriticalWindow   = 7 * 24 * time.Hour
)

func (c *Client) EndpointCertificate(ctx context.Context) (domain.KubernetesEndpointCertificate, error) {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + "/version"
	requestURL.RawQuery = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return domain.KubernetesEndpointCertificate{}, fmt.Errorf("create Kubernetes request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return domain.KubernetesEndpointCertificate{}, fmt.Errorf("Kubernetes request canceled: %w", context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.KubernetesEndpointCertificate{}, fmt.Errorf("Kubernetes request: %w", domain.ErrTimeout)
		}
		return domain.KubernetesEndpointCertificate{}, fmt.Errorf("Kubernetes request: %w", domain.ErrUpstream)
	}
	if response.Body == nil {
		return domain.KubernetesEndpointCertificate{}, fmt.Errorf("Kubernetes response body unavailable: %w", domain.ErrUpstream)
	}
	defer response.Body.Close()
	if err := endpointCertificateStatusError(response.StatusCode); err != nil {
		return domain.KubernetesEndpointCertificate{}, err
	}

	leaf, ok := verifiedEndpointCertificateLeaf(response.TLS)
	if !ok {
		return domain.KubernetesEndpointCertificate{}, fmt.Errorf("Kubernetes endpoint certificate unavailable: %w", domain.ErrUpstream)
	}
	observedAt := time.Now().UTC()
	written, err := io.Copy(io.Discard, io.LimitReader(response.Body, endpointCertificateMaxResponseBytes+1))
	if err != nil {
		return domain.KubernetesEndpointCertificate{}, fmt.Errorf("read Kubernetes response: %w", domain.ErrUpstream)
	}
	if written > endpointCertificateMaxResponseBytes {
		return domain.KubernetesEndpointCertificate{}, fmt.Errorf("Kubernetes response exceeded size limit: %w", domain.ErrUpstream)
	}
	return projectEndpointCertificate(leaf, observedAt), nil
}

func verifiedEndpointCertificateLeaf(state *tls.ConnectionState) (*x509.Certificate, bool) {
	if state == nil || !state.HandshakeComplete || len(state.PeerCertificates) == 0 || state.PeerCertificates[0] == nil {
		return nil, false
	}
	leaf := state.PeerCertificates[0]
	for _, chain := range state.VerifiedChains {
		if len(chain) > 0 && chain[0] != nil && bytes.Equal(chain[0].Raw, leaf.Raw) {
			return leaf, true
		}
	}
	return nil, false
}

func projectEndpointCertificate(certificate *x509.Certificate, observedAt time.Time) domain.KubernetesEndpointCertificate {
	observedAt = observedAt.UTC()
	notBefore := certificate.NotBefore.UTC()
	notAfter := certificate.NotAfter.UTC()
	remaining := notAfter.Sub(observedAt)
	remainingSeconds := int64(remaining / time.Second)
	if remaining > 0 && remaining%time.Second != 0 {
		remainingSeconds++
	}

	status := domain.EndpointCertificateExpired
	switch {
	case remaining > endpointCertificateExpiringWindow:
		status = domain.EndpointCertificateValid
	case remaining > endpointCertificateCriticalWindow:
		status = domain.EndpointCertificateExpiring
	case remaining > 0:
		status = domain.EndpointCertificateCritical
	}
	return domain.KubernetesEndpointCertificate{
		ObservedAt:       observedAt,
		NotBefore:        notBefore,
		NotAfter:         notAfter,
		RemainingSeconds: remainingSeconds,
		Status:           status,
	}
}

func endpointCertificateStatusError(statusCode int) error {
	switch {
	case statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices:
		return nil
	case statusCode == http.StatusUnauthorized:
		return fmt.Errorf("Kubernetes authentication rejected: %w", domain.ErrUnauthorized)
	case statusCode == http.StatusForbidden:
		return fmt.Errorf("Kubernetes authorization rejected: %w", domain.ErrForbidden)
	case statusCode == http.StatusNotFound:
		return fmt.Errorf("Kubernetes resource unavailable: %w", domain.ErrNotFound)
	case statusCode == http.StatusConflict:
		return fmt.Errorf("Kubernetes resource version conflict: %w", domain.ErrConflict)
	default:
		return fmt.Errorf("Kubernetes returned HTTP %d: %w", statusCode, domain.ErrUpstream)
	}
}
