package helmadapter

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/registry"
	"oras.land/oras-go/v2/registry/remote/auth"

	"github.com/caoyanyi/k8s-panel/internal/outbound"
)

func loadOCIChart(
	ctx context.Context,
	reference string,
	version string,
	policy *outbound.Policy,
	roots *x509.CertPool,
	timeout time.Duration,
) (*chart.Chart, error) {
	if policy == nil {
		return nil, errors.New("outbound policy is required")
	}
	validationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	httpsReference := "https://" + strings.TrimPrefix(reference, "oci://")
	if _, err := policy.ValidateHTTPSURL(validationContext, httpsReference); err != nil {
		return nil, fmt.Errorf("validate OCI registry: %w", err)
	}
	parsed, err := url.Parse(reference)
	if err != nil {
		return nil, fmt.Errorf("parse OCI reference: %w", err)
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           policy.DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   4,
	}
	httpClient := newPolicyHTTPClient(policy, transport, timeout)
	authorizer := auth.Client{
		Client: httpClient,
		Credential: func(context.Context, string) (auth.Credential, error) {
			return auth.EmptyCredential, nil
		},
	}
	registryClient, err := registry.NewClient(
		registry.ClientOptHTTPClient(httpClient),
		registry.ClientOptAuthorizer(authorizer),
		registry.ClientOptWriter(io.Discard),
	)
	if err != nil {
		return nil, fmt.Errorf("create OCI registry client: %w", err)
	}
	resolved, err := registryClient.ValidateReference(reference, version, parsed)
	if err != nil {
		return nil, fmt.Errorf("resolve OCI chart version: %w", err)
	}
	result, err := registryClient.Pull(strings.TrimPrefix(resolved.String(), "oci://"))
	if err != nil {
		return nil, fmt.Errorf("pull OCI chart: %w", err)
	}
	if result.Chart == nil || len(result.Chart.Data) == 0 {
		return nil, errors.New("OCI artifact contains no Helm chart")
	}
	if len(result.Chart.Data) > maxChartArchiveBytes {
		return nil, errors.New("OCI chart exceeds size limit")
	}
	return loader.LoadArchive(bytes.NewReader(result.Chart.Data))
}
