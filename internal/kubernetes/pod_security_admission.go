package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	podSecurityAdmissionListPageSize                = "250"
	maxPodSecurityAdmissionListPages                = 4
	maxPodSecurityAdmissionListItems                = 1000
	maxPodSecurityAdmissionPageBytes          int64 = 2 * 1024 * 1024
	maxPodSecurityAdmissionListBytes          int64 = 4 * 1024 * 1024
	maxPodSecurityAdmissionContinueBytes            = 16 * 1024
	maxPodSecurityAdmissionLabelsPerNamespace       = 256
)

const (
	podSecurityAdmissionLabelPrefix = "pod-security.kubernetes.io/"
	podSecurityAdmissionEnforce     = "enforce"
	podSecurityAdmissionAudit       = "audit"
	podSecurityAdmissionWarn        = "warn"
)

type podSecurityAdmissionMetadata struct {
	Name              string
	Labels            map[string]json.RawMessage
	CreationTimestamp time.Time
}

func (c *Client) PodSecurityAdmissionNamespaces(
	ctx context.Context,
) ([]domain.KubernetesPodSecurityAdmissionNamespace, error) {
	query := url.Values{"limit": {podSecurityAdmissionListPageSize}}
	items := make([]domain.KubernetesPodSecurityAdmissionNamespace, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64

	for page := 0; page < maxPodSecurityAdmissionListPages; page++ {
		remainingBytes := maxPodSecurityAdmissionListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes Pod Security Admission list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx,
			"/api/v1/namespaces",
			query,
			kubernetesPartialMetadataListAccept,
			min(remainingBytes, maxPodSecurityAdmissionPageBytes),
			false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes Pod Security Admission metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes Pod Security Admission metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > maxPodSecurityAdmissionListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes Pod Security Admission list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePodSecurityAdmissionMetadata(raw)
			if err != nil {
				return nil, err
			}
			if _, exists := seenNames[metadata.Name]; exists {
				return nil, fmt.Errorf("duplicate Kubernetes Pod Security Admission namespace: %w", domain.ErrUpstream)
			}
			seenNames[metadata.Name] = struct{}{}
			items = append(items, projectPodSecurityAdmissionNamespace(metadata))
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validPodSecurityAdmissionContinuation(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes Pod Security Admission continuation token: %w", domain.ErrUpstream)
		}
		if _, exists := seenContinue[continuation]; exists {
			return nil, fmt.Errorf("repeated Kubernetes Pod Security Admission continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		if len(items) >= maxPodSecurityAdmissionListItems {
			return nil, fmt.Errorf("Kubernetes Pod Security Admission list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes Pod Security Admission list exceeded safe page limit: %w", domain.ErrUpstream)
}

func decodePodSecurityAdmissionMetadata(raw json.RawMessage) (podSecurityAdmissionMetadata, error) {
	var empty podSecurityAdmissionMetadata
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil {
		return empty, fmt.Errorf("decode Kubernetes Pod Security Admission metadata: %w", domain.ErrUpstream)
	}
	for field := range fields {
		if field != "apiVersion" && field != "kind" && field != "metadata" {
			return empty, fmt.Errorf("unsafe Kubernetes Pod Security Admission metadata object: %w", domain.ErrUpstream)
		}
	}

	var envelope struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name              string                     `json:"name"`
			Namespace         string                     `json:"namespace"`
			Labels            map[string]json.RawMessage `json:"labels"`
			CreationTimestamp time.Time                  `json:"creationTimestamp"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.APIVersion != "meta.k8s.io/v1" ||
		envelope.Kind != "PartialObjectMetadata" {
		return empty, fmt.Errorf("unsupported Kubernetes Pod Security Admission metadata object: %w", domain.ErrUpstream)
	}
	if domain.ValidateNamespace(envelope.Metadata.Name) != nil || envelope.Metadata.Namespace != "" ||
		envelope.Metadata.CreationTimestamp.IsZero() {
		return empty, fmt.Errorf("invalid Kubernetes Pod Security Admission namespace identity: %w", domain.ErrUpstream)
	}
	if len(envelope.Metadata.Labels) > maxPodSecurityAdmissionLabelsPerNamespace {
		return empty, fmt.Errorf("Kubernetes Pod Security Admission metadata exceeded safe label limit: %w", domain.ErrUpstream)
	}
	return podSecurityAdmissionMetadata{
		Name:              envelope.Metadata.Name,
		Labels:            envelope.Metadata.Labels,
		CreationTimestamp: envelope.Metadata.CreationTimestamp,
	}, nil
}

func projectPodSecurityAdmissionNamespace(
	metadata podSecurityAdmissionMetadata,
) domain.KubernetesPodSecurityAdmissionNamespace {
	enforce := projectPodSecurityAdmissionMode(metadata.Labels, podSecurityAdmissionEnforce)
	audit := projectPodSecurityAdmissionMode(metadata.Labels, podSecurityAdmissionAudit)
	warn := projectPodSecurityAdmissionMode(metadata.Labels, podSecurityAdmissionWarn)
	invalidModeCount := 0
	for _, mode := range []domain.KubernetesPodSecurityAdmissionMode{enforce, audit, warn} {
		if mode.Status == domain.PodSecurityAdmissionModeInvalid {
			invalidModeCount++
		}
	}
	return domain.KubernetesPodSecurityAdmissionNamespace{
		Name: metadata.Name, Enforce: enforce, Audit: audit, Warn: warn,
		InvalidModeCount: invalidModeCount, CreatedAt: metadata.CreationTimestamp,
	}
}

func projectPodSecurityAdmissionMode(
	labels map[string]json.RawMessage,
	mode string,
) domain.KubernetesPodSecurityAdmissionMode {
	level, levelPresent, levelJSONValid := podSecurityAdmissionLabel(labels, podSecurityAdmissionLabelPrefix+mode)
	version, versionPresent, versionJSONValid := podSecurityAdmissionLabel(labels, podSecurityAdmissionLabelPrefix+mode+"-version")
	if !levelJSONValid || !versionJSONValid {
		return domain.KubernetesPodSecurityAdmissionMode{Status: domain.PodSecurityAdmissionModeInvalid}
	}
	if !levelPresent && !versionPresent {
		return domain.KubernetesPodSecurityAdmissionMode{Status: domain.PodSecurityAdmissionModeInherited}
	}
	if !levelPresent || !validPodSecurityAdmissionLevel(level) {
		return domain.KubernetesPodSecurityAdmissionMode{Status: domain.PodSecurityAdmissionModeInvalid}
	}
	if !versionPresent {
		return domain.KubernetesPodSecurityAdmissionMode{
			Status: domain.PodSecurityAdmissionModeConfigured, Level: level, Version: "latest", VersionDefaulted: true,
		}
	}
	if !validPodSecurityAdmissionVersion(version) {
		return domain.KubernetesPodSecurityAdmissionMode{Status: domain.PodSecurityAdmissionModeInvalid}
	}
	return domain.KubernetesPodSecurityAdmissionMode{
		Status: domain.PodSecurityAdmissionModeConfigured, Level: level, Version: version,
	}
}

func podSecurityAdmissionLabel(labels map[string]json.RawMessage, key string) (string, bool, bool) {
	raw, exists := labels[key]
	if !exists {
		return "", false, true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", true, false
	}
	return value, true, true
}

func validPodSecurityAdmissionLevel(value string) bool {
	return value == "privileged" || value == "baseline" || value == "restricted"
}

func validPodSecurityAdmissionVersion(value string) bool {
	if value == "latest" {
		return true
	}
	if len(value) < 4 || len(value) > 16 || !strings.HasPrefix(value, "v1.") {
		return false
	}
	minor := value[3:]
	if len(minor) > 1 && minor[0] == '0' {
		return false
	}
	for _, digit := range minor {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func validPodSecurityAdmissionContinuation(value string) bool {
	return value != "" && len(value) <= maxPodSecurityAdmissionContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
