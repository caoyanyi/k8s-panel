package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	apiServerReadinessMaxResponseBytes int64 = 64 * 1024
	apiServerReadinessMaxLineBytes           = 8 * 1024
	apiServerReadinessMaxChecks              = 256
	apiServerReadinessMaxNameBytes           = 253
)

func (c *Client) APIServerReadiness(ctx context.Context) (domain.KubernetesAPIServerReadiness, error) {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + "/readyz"
	requestURL.RawQuery = "verbose"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("create Kubernetes request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "text/plain")

	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("Kubernetes request canceled: %w", context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("Kubernetes request: %w", domain.ErrTimeout)
		}
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("Kubernetes request: %w", domain.ErrUpstream)
	}
	if response.Body == nil {
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("Kubernetes response body unavailable: %w", domain.ErrUpstream)
	}
	defer response.Body.Close()
	if err := apiServerReadinessStatusError(response.StatusCode); err != nil {
		return domain.KubernetesAPIServerReadiness{}, err
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, apiServerReadinessMaxResponseBytes+1))
	if err != nil {
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("read Kubernetes response: %w", domain.ErrUpstream)
	}
	if int64(len(payload)) > apiServerReadinessMaxResponseBytes {
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("Kubernetes response exceeded size limit: %w", domain.ErrUpstream)
	}
	return parseAPIServerReadiness(payload, response.StatusCode, time.Now().UTC())
}

func apiServerReadinessStatusError(statusCode int) error {
	switch statusCode {
	case http.StatusOK, http.StatusInternalServerError:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("Kubernetes authentication rejected: %w", domain.ErrUnauthorized)
	case http.StatusForbidden:
		return fmt.Errorf("Kubernetes authorization rejected: %w", domain.ErrForbidden)
	case http.StatusNotFound:
		return fmt.Errorf("Kubernetes resource unavailable: %w", domain.ErrNotFound)
	default:
		return fmt.Errorf("Kubernetes returned HTTP %d: %w", statusCode, domain.ErrUpstream)
	}
}

func parseAPIServerReadiness(
	payload []byte,
	statusCode int,
	observedAt time.Time,
) (domain.KubernetesAPIServerReadiness, error) {
	if statusCode != http.StatusOK && statusCode != http.StatusInternalServerError {
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("unsupported Kubernetes readiness status: %w", domain.ErrUpstream)
	}
	checks := make([]domain.KubernetesAPIServerReadinessCheck, 0)
	seen := make(map[string]struct{})
	passedChecks := 0
	failedChecks := 0
	summaryFound := false
	summaryReady := false

	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 4096), apiServerReadinessMaxLineBytes+1)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if len(line) > apiServerReadinessMaxLineBytes {
			return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("Kubernetes readiness line exceeded safe limit: %w", domain.ErrUpstream)
		}
		if ready, summary := apiServerReadinessSummary(line); summary {
			if summaryFound {
				return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("duplicate Kubernetes readiness summary: %w", domain.ErrUpstream)
			}
			summaryFound = true
			summaryReady = ready
			continue
		}
		check, ok := parseAPIServerReadinessCheck(line)
		if !ok {
			return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("invalid Kubernetes readiness output: %w", domain.ErrUpstream)
		}
		if len(checks) >= apiServerReadinessMaxChecks {
			return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("Kubernetes readiness checks exceeded safe limit: %w", domain.ErrUpstream)
		}
		if _, exists := seen[check.Name]; exists {
			return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("duplicate Kubernetes readiness check: %w", domain.ErrUpstream)
		}
		seen[check.Name] = struct{}{}
		checks = append(checks, check)
		if check.Status == domain.APIServerReadinessCheckPassed {
			passedChecks++
		} else {
			failedChecks++
		}
	}
	if scanner.Err() != nil {
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("scan Kubernetes readiness output: %w", domain.ErrUpstream)
	}
	ready := statusCode == http.StatusOK
	if len(checks) == 0 || !summaryFound || summaryReady != ready ||
		(ready && failedChecks != 0) || (!ready && failedChecks == 0) {
		return domain.KubernetesAPIServerReadiness{}, fmt.Errorf("inconsistent Kubernetes readiness output: %w", domain.ErrUpstream)
	}
	return domain.KubernetesAPIServerReadiness{
		ObservedAt:   observedAt.UTC(),
		Ready:        ready,
		PassedChecks: passedChecks,
		FailedChecks: failedChecks,
		Checks:       checks,
	}, nil
}

func apiServerReadinessSummary(line string) (bool, bool) {
	switch line {
	case "readyz check passed", "healthz check passed":
		return true, true
	case "readyz check failed", "healthz check failed":
		return false, true
	default:
		return false, false
	}
}

func parseAPIServerReadinessCheck(line string) (domain.KubernetesAPIServerReadinessCheck, bool) {
	var empty domain.KubernetesAPIServerReadinessCheck
	if strings.HasPrefix(line, "[+]") && strings.HasSuffix(line, " ok") {
		name := strings.TrimSuffix(strings.TrimPrefix(line, "[+]"), " ok")
		if !validAPIServerReadinessCheckName(name) {
			return empty, false
		}
		return domain.KubernetesAPIServerReadinessCheck{Name: name, Status: domain.APIServerReadinessCheckPassed}, true
	}
	if !strings.HasPrefix(line, "[-]") {
		return empty, false
	}
	remainder := strings.TrimPrefix(line, "[-]")
	failureIndex := strings.Index(remainder, " failed")
	if failureIndex <= 0 {
		return empty, false
	}
	name := remainder[:failureIndex]
	detail := remainder[failureIndex+len(" failed"):]
	if (detail != "" && !strings.HasPrefix(detail, ":")) || !validAPIServerReadinessCheckName(name) {
		return empty, false
	}
	return domain.KubernetesAPIServerReadinessCheck{Name: name, Status: domain.APIServerReadinessCheckFailed}, true
}

func validAPIServerReadinessCheckName(name string) bool {
	if name == "" || len(name) > apiServerReadinessMaxNameBytes {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '.' || character == '_' ||
			character == '-' || character == '/' || character == ':' {
			continue
		}
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
