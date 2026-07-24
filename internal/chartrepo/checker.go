package chartrepo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/outbound"
	"github.com/caoyanyi/k8s-panel/internal/platform"
)

const maxIndexBytes = 2 * 1024 * 1024

type Checker struct {
	policy *outbound.Policy
	roots  *x509.CertPool
}

func NewChecker(policy *outbound.Policy, roots *x509.CertPool) *Checker {
	return &Checker{policy: policy, roots: roots}
}

func (c *Checker) Check(ctx context.Context, connection platform.RepositoryConnection) error {
	if c.policy == nil {
		return errors.New("outbound policy is required")
	}
	baseURL, err := c.policy.ValidateHTTPSURL(ctx, connection.URL)
	if err != nil {
		return domain.ErrUpstream
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/index.yaml"
	baseURL.RawQuery = ""
	baseURL.Fragment = ""

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create repository request: %w", err)
	}
	request.Header.Set("Accept", "application/yaml, text/yaml, text/plain")
	if connection.Username != "" {
		request.SetBasicAuth(connection.Username, connection.Password)
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           c.policy.DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.roots},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return domain.ErrUpstream
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.ErrUpstream
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxIndexBytes+1))
	if err != nil || len(payload) > maxIndexBytes {
		return domain.ErrUpstream
	}
	var index struct {
		APIVersion string                         `yaml:"apiVersion"`
		Entries    map[string][]repositoryVersion `yaml:"entries"`
	}
	if err := yaml.Unmarshal(payload, &index); err != nil {
		return domain.ErrUpstream
	}
	if strings.TrimSpace(index.APIVersion) == "" || len(index.Entries) == 0 {
		return errors.New("repository index contains no charts")
	}
	return nil
}

type repositoryVersion struct {
	Version string `yaml:"version"`
}
