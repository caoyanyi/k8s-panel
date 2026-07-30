package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	helmReleaseHistoryPageSize               = "50"
	helmReleaseHistoryMaxPages               = 4
	helmReleaseHistoryMaxItems               = 200
	helmReleaseHistoryMaxResults             = 10
	helmReleaseHistoryMaxContinueBytes       = 16 * 1024
	helmReleaseHistoryMaxBytes         int64 = 2 * 1024 * 1024
)

type helmReleaseMetadata struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
	} `json:"metadata"`
}

func (c *Client) HelmReleaseHistory(
	ctx context.Context,
	namespace, releaseName string,
) (domain.HelmReleaseHistory, error) {
	if err := domain.ValidateHelmReleaseReference(namespace, releaseName); err != nil {
		return domain.HelmReleaseHistory{}, err
	}

	path := "/api/v1/namespaces/" + namespace + "/secrets"
	query := url.Values{
		"limit":         {helmReleaseHistoryPageSize},
		"labelSelector": {"owner=helm,name=" + releaseName},
	}
	revisions := make([]domain.HelmReleaseRevision, 0)
	seenRevisions := make(map[int]struct{})
	seenContinuations := make(map[string]struct{})
	var totalBytes int64

	for page := 0; page < helmReleaseHistoryMaxPages; page++ {
		remainingBytes := helmReleaseHistoryMaxBytes - totalBytes
		if remainingBytes <= 0 {
			return domain.HelmReleaseHistory{}, fmt.Errorf("Helm release history exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx, path, query, kubernetesPartialMetadataListAccept, remainingBytes, false,
		)
		if err != nil {
			return domain.HelmReleaseHistory{}, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return domain.HelmReleaseHistory{}, fmt.Errorf("decode Helm release metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return domain.HelmReleaseHistory{}, fmt.Errorf("unsupported Helm release metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > helmReleaseHistoryMaxItems-len(revisions) {
			return domain.HelmReleaseHistory{}, fmt.Errorf("Helm release history exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			revision, err := decodeHelmReleaseRevision(raw, namespace, releaseName)
			if err != nil {
				return domain.HelmReleaseHistory{}, err
			}
			if _, duplicate := seenRevisions[revision.Revision]; duplicate {
				return domain.HelmReleaseHistory{}, fmt.Errorf("duplicate Helm release revision metadata: %w", domain.ErrUpstream)
			}
			seenRevisions[revision.Revision] = struct{}{}
			revisions = append(revisions, revision)
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			return boundedHelmReleaseHistory(namespace, releaseName, revisions), nil
		}
		if !validHelmReleaseHistoryContinuation(continuation) {
			return domain.HelmReleaseHistory{}, fmt.Errorf("invalid Helm release history continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinuations[continuation]; duplicate {
			return domain.HelmReleaseHistory{}, fmt.Errorf("repeated Helm release history continuation token: %w", domain.ErrUpstream)
		}
		seenContinuations[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return domain.HelmReleaseHistory{}, fmt.Errorf("Helm release history exceeded safe page limit: %w", domain.ErrUpstream)
}

func decodeHelmReleaseRevision(
	raw json.RawMessage,
	namespace, releaseName string,
) (domain.HelmReleaseRevision, error) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil {
		return domain.HelmReleaseRevision{}, fmt.Errorf("decode Helm release metadata: %w", domain.ErrUpstream)
	}
	for field := range fields {
		if field != "apiVersion" && field != "kind" && field != "metadata" {
			return domain.HelmReleaseRevision{}, fmt.Errorf("unsafe Helm release metadata: %w", domain.ErrUpstream)
		}
	}

	var source helmReleaseMetadata
	if err := json.Unmarshal(raw, &source); err != nil ||
		source.APIVersion != "meta.k8s.io/v1" || source.Kind != "PartialObjectMetadata" ||
		source.Metadata.Namespace != namespace || source.Metadata.CreationTimestamp.IsZero() {
		return domain.HelmReleaseRevision{}, fmt.Errorf("invalid Helm release metadata identity: %w", domain.ErrUpstream)
	}
	labels := source.Metadata.Labels
	if labels["owner"] != "helm" || labels["name"] != releaseName {
		return domain.HelmReleaseRevision{}, fmt.Errorf("invalid Helm release metadata labels: %w", domain.ErrUpstream)
	}
	revisionValue, err := strconv.ParseInt(labels["version"], 10, 32)
	if err != nil || revisionValue <= 0 {
		return domain.HelmReleaseRevision{}, fmt.Errorf("invalid Helm release revision: %w", domain.ErrUpstream)
	}
	revision := int(revisionValue)
	if source.Metadata.Name != fmt.Sprintf("sh.helm.release.v1.%s.v%d", releaseName, revision) {
		return domain.HelmReleaseRevision{}, fmt.Errorf("invalid Helm release storage identity: %w", domain.ErrUpstream)
	}
	status := labels["status"]
	if !validHelmReleaseStatus(status) {
		return domain.HelmReleaseRevision{}, fmt.Errorf("invalid Helm release status: %w", domain.ErrUpstream)
	}
	return domain.HelmReleaseRevision{
		Revision: revision, Status: status, CreatedAt: source.Metadata.CreationTimestamp.UTC(),
	}, nil
}

func boundedHelmReleaseHistory(
	namespace, releaseName string,
	revisions []domain.HelmReleaseRevision,
) domain.HelmReleaseHistory {
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision > revisions[j].Revision })
	truncated := len(revisions) > helmReleaseHistoryMaxResults
	if truncated {
		revisions = revisions[:helmReleaseHistoryMaxResults]
	}
	result := make([]domain.HelmReleaseRevision, len(revisions))
	copy(result, revisions)
	return domain.HelmReleaseHistory{
		Name: releaseName, Namespace: namespace,
		Revisions: result, Truncated: truncated,
	}
}

func validHelmReleaseStatus(status string) bool {
	switch status {
	case "unknown", "deployed", "uninstalled", "superseded", "failed", "uninstalling",
		"pending-install", "pending-upgrade", "pending-rollback":
		return true
	default:
		return false
	}
}

func validHelmReleaseHistoryContinuation(value string) bool {
	return value != "" && len(value) <= helmReleaseHistoryMaxContinueBytes &&
		value == strings.TrimSpace(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}
