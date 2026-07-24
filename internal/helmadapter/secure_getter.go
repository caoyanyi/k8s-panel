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
	"time"

	"helm.sh/helm/v3/pkg/getter"

	"github.com/caoyanyi/k8s-panel/internal/outbound"
)

const maxChartArchiveBytes = 64 * 1024 * 1024

type secureHTTPGetter struct {
	ctx            context.Context
	policy         *outbound.Policy
	repositoryHost string
	username       string
	password       string
	client         *http.Client
}

func newSecureHTTPGetter(
	ctx context.Context,
	policy *outbound.Policy,
	roots *x509.CertPool,
	repositoryURL string,
	username string,
	password string,
	timeout time.Duration,
) (getter.Getter, error) {
	if policy == nil {
		return nil, errors.New("outbound policy is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	validationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	parsed, err := policy.ValidateHTTPSURL(validationContext, repositoryURL)
	if err != nil {
		return nil, fmt.Errorf("validate chart repository: %w", err)
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
	result := &secureHTTPGetter{
		ctx:            ctx,
		policy:         policy,
		repositoryHost: parsed.Host,
		username:       username,
		password:       password,
	}
	result.client = newPolicyHTTPClient(policy, transport, timeout)
	result.client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("chart download exceeded redirect limit")
		}
		result.setAuthorization(request)
		return nil
	}
	return result, nil
}

func newPolicyHTTPClient(policy *outbound.Policy, transport *http.Transport, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: policyRoundTripper{policy: policy, next: transport, maxBytes: maxChartArchiveBytes},
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("outbound request exceeded redirect limit")
			}
			return nil
		},
	}
}

func (g *secureHTTPGetter) Get(rawURL string, _ ...getter.Option) (*bytes.Buffer, error) {
	requestContext, cancel := context.WithTimeout(g.ctx, g.client.Timeout)
	defer cancel()
	parsed, err := g.policy.ValidateHTTPSURL(requestContext, rawURL)
	if err != nil {
		return nil, fmt.Errorf("validate chart URL: %w", err)
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create chart request: %w", err)
	}
	request.Header.Set("Accept", "application/gzip, application/octet-stream, application/yaml, text/yaml")
	g.setAuthorization(request)
	response, err := g.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download chart content: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("chart server returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxChartArchiveBytes {
		return nil, errors.New("chart content exceeds size limit")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxChartArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read chart content: %w", err)
	}
	if len(payload) > maxChartArchiveBytes {
		return nil, errors.New("chart content exceeds size limit")
	}
	return bytes.NewBuffer(payload), nil
}

func (g *secureHTTPGetter) setAuthorization(request *http.Request) {
	request.Header.Del("Authorization")
	if g.username != "" && request.URL.Host == g.repositoryHost {
		request.SetBasicAuth(g.username, g.password)
	}
}

type policyRoundTripper struct {
	policy   *outbound.Policy
	next     http.RoundTripper
	maxBytes int64
}

func (t policyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if _, err := t.policy.ValidateHTTPSURL(request.Context(), request.URL.String()); err != nil {
		return nil, fmt.Errorf("outbound request is blocked: %w", err)
	}
	response, err := t.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > t.maxBytes {
		response.Body.Close()
		return nil, errors.New("outbound response exceeds size limit")
	}
	response.Body = &limitedReadCloser{
		Reader: io.LimitReader(response.Body, t.maxBytes+1),
		Closer: response.Body,
	}
	return response, nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}
