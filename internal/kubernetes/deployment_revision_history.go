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
	deploymentRevisionAnnotation               = "deployment.kubernetes.io/revision"
	deploymentRevisionPageSize                 = "250"
	deploymentRevisionMaxPages                 = 4
	deploymentRevisionMaxItems                 = 1000
	deploymentRevisionMaxResults               = 20
	deploymentRevisionMaxOwnerReferences       = 16
	deploymentRevisionMaxAnnotations           = 256
	deploymentRevisionMaxContinueBytes         = 16 * 1024
	deploymentRevisionMaxUIDBytes              = 128
	deploymentRevisionMaxObjectBytes     int64 = 256 * 1024
	deploymentRevisionMaxPageBytes       int64 = 2 * 1024 * 1024
	deploymentRevisionMaxListBytes       int64 = 4 * 1024 * 1024
)

type deploymentRevisionMetadataEnvelope struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		UID               string            `json:"uid"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
		Annotations       map[string]string `json:"annotations"`
		OwnerReferences   []struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			UID        string `json:"uid"`
			Controller *bool  `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
}

func (c *Client) DeploymentRevisionHistory(
	ctx context.Context,
	reference domain.WorkloadReference,
) (domain.DeploymentRevisionHistory, error) {
	reference.Kind = strings.ToLower(strings.TrimSpace(reference.Kind))
	if err := domain.ValidateDeploymentReference(reference); err != nil {
		return domain.DeploymentRevisionHistory{}, err
	}

	collectionPath := "/apis/apps/v1/namespaces/" + url.PathEscape(reference.Namespace)
	deploymentPath := collectionPath + "/deployments/" + url.PathEscape(reference.Name)
	payload, _, err := c.getPayload(
		ctx, deploymentPath, nil, kubernetesPartialMetadataAccept, deploymentRevisionMaxObjectBytes, false,
	)
	if err != nil {
		return domain.DeploymentRevisionHistory{}, err
	}
	deployment, err := decodeDeploymentRevisionMetadata(payload, reference)
	if err != nil {
		return domain.DeploymentRevisionHistory{}, err
	}
	currentRevision, err := optionalDeploymentRevision(deployment.Metadata.Annotations)
	if err != nil {
		return domain.DeploymentRevisionHistory{}, err
	}

	replicaSetPath := collectionPath + "/replicasets"
	query := url.Values{"limit": {deploymentRevisionPageSize}}
	revisions := make([]domain.DeploymentRevision, 0)
	seenRevisions := make(map[int]struct{})
	seenContinuations := make(map[string]struct{})
	totalItems := 0
	unassigned := 0
	var totalBytes int64

	for page := 0; page < deploymentRevisionMaxPages; page++ {
		remainingBytes := deploymentRevisionMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return domain.DeploymentRevisionHistory{}, fmt.Errorf("Deployment revision history exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		pageBytes := min(remainingBytes, deploymentRevisionMaxPageBytes)
		payload, _, err := c.getPayload(
			ctx, replicaSetPath, query, kubernetesPartialMetadataListAccept, pageBytes, false,
		)
		if err != nil {
			return domain.DeploymentRevisionHistory{}, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return domain.DeploymentRevisionHistory{}, fmt.Errorf("decode Deployment revision metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return domain.DeploymentRevisionHistory{}, fmt.Errorf("unsupported Deployment revision metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > deploymentRevisionMaxItems-totalItems {
			return domain.DeploymentRevisionHistory{}, fmt.Errorf("Deployment revision history exceeded safe item limit: %w", domain.ErrUpstream)
		}
		totalItems += len(response.Items)
		for _, raw := range response.Items {
			revision, owned, assigned, err := decodeDeploymentReplicaSetRevision(
				raw, reference, deployment.Metadata.UID, currentRevision,
			)
			if err != nil {
				return domain.DeploymentRevisionHistory{}, err
			}
			if !owned {
				continue
			}
			if !assigned {
				unassigned++
				continue
			}
			if _, duplicate := seenRevisions[revision.Revision]; duplicate {
				return domain.DeploymentRevisionHistory{}, fmt.Errorf("duplicate Deployment revision metadata: %w", domain.ErrUpstream)
			}
			seenRevisions[revision.Revision] = struct{}{}
			revisions = append(revisions, revision)
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			return boundedDeploymentRevisionHistory(reference, currentRevision, unassigned, revisions), nil
		}
		if !validDeploymentRevisionContinuation(continuation) {
			return domain.DeploymentRevisionHistory{}, fmt.Errorf("invalid Deployment revision continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinuations[continuation]; duplicate {
			return domain.DeploymentRevisionHistory{}, fmt.Errorf("repeated Deployment revision continuation token: %w", domain.ErrUpstream)
		}
		seenContinuations[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return domain.DeploymentRevisionHistory{}, fmt.Errorf("Deployment revision history exceeded safe page limit: %w", domain.ErrUpstream)
}

func decodeDeploymentRevisionMetadata(
	raw json.RawMessage,
	reference domain.WorkloadReference,
) (deploymentRevisionMetadataEnvelope, error) {
	metadata, err := decodeStrictDeploymentRevisionMetadata(raw)
	if err != nil {
		return deploymentRevisionMetadataEnvelope{}, err
	}
	if metadata.Metadata.Name != reference.Name || metadata.Metadata.Namespace != reference.Namespace ||
		!validDeploymentRevisionUID(metadata.Metadata.UID) || metadata.Metadata.CreationTimestamp.IsZero() {
		return deploymentRevisionMetadataEnvelope{}, fmt.Errorf("invalid Deployment revision identity: %w", domain.ErrUpstream)
	}
	return metadata, nil
}

func decodeDeploymentReplicaSetRevision(
	raw json.RawMessage,
	reference domain.WorkloadReference,
	deploymentUID string,
	currentRevision int,
) (domain.DeploymentRevision, bool, bool, error) {
	metadata, err := decodeStrictDeploymentRevisionMetadata(raw)
	if err != nil {
		return domain.DeploymentRevision{}, false, false, err
	}
	if metadata.Metadata.Namespace != reference.Namespace ||
		domain.ValidateWorkloadReference(domain.WorkloadReference{
			Kind: "deployment", Namespace: reference.Namespace, Name: metadata.Metadata.Name,
		}) != nil ||
		!validDeploymentRevisionUID(metadata.Metadata.UID) || metadata.Metadata.CreationTimestamp.IsZero() {
		return domain.DeploymentRevision{}, false, false, fmt.Errorf("invalid ReplicaSet revision identity: %w", domain.ErrUpstream)
	}

	controllerCount := 0
	owned := false
	for _, owner := range metadata.Metadata.OwnerReferences {
		if owner.Controller == nil || !*owner.Controller {
			continue
		}
		controllerCount++
		if !validDeploymentRevisionOwner(owner.APIVersion, owner.Kind, owner.Name, owner.UID) {
			return domain.DeploymentRevision{}, false, false, fmt.Errorf("invalid ReplicaSet controller owner: %w", domain.ErrUpstream)
		}
		owned = owner.APIVersion == "apps/v1" && owner.Kind == "Deployment" &&
			owner.Name == reference.Name && owner.UID == deploymentUID
	}
	if controllerCount > 1 {
		return domain.DeploymentRevision{}, false, false, fmt.Errorf("multiple ReplicaSet controller owners: %w", domain.ErrUpstream)
	}
	if !owned {
		return domain.DeploymentRevision{}, false, false, nil
	}

	revisionValue, exists := metadata.Metadata.Annotations[deploymentRevisionAnnotation]
	if !exists {
		return domain.DeploymentRevision{}, true, false, nil
	}
	revision, err := canonicalDeploymentRevision(revisionValue)
	if err != nil {
		return domain.DeploymentRevision{}, false, false, err
	}
	return domain.DeploymentRevision{
		Revision: revision, ReplicaSet: metadata.Metadata.Name,
		CreatedAt: metadata.Metadata.CreationTimestamp.UTC(), Current: currentRevision > 0 && revision == currentRevision,
	}, true, true, nil
}

func decodeStrictDeploymentRevisionMetadata(raw json.RawMessage) (deploymentRevisionMetadataEnvelope, error) {
	var empty deploymentRevisionMetadataEnvelope
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil {
		return empty, fmt.Errorf("decode Deployment revision metadata: %w", domain.ErrUpstream)
	}
	for field := range fields {
		if field != "apiVersion" && field != "kind" && field != "metadata" {
			return empty, fmt.Errorf("unsafe Deployment revision metadata: %w", domain.ErrUpstream)
		}
	}
	var metadata deploymentRevisionMetadataEnvelope
	if err := json.Unmarshal(raw, &metadata); err != nil ||
		metadata.APIVersion != "meta.k8s.io/v1" || metadata.Kind != "PartialObjectMetadata" {
		return empty, fmt.Errorf("unsupported Deployment revision metadata: %w", domain.ErrUpstream)
	}
	if len(metadata.Metadata.OwnerReferences) > deploymentRevisionMaxOwnerReferences ||
		len(metadata.Metadata.Annotations) > deploymentRevisionMaxAnnotations {
		return empty, fmt.Errorf("Deployment revision metadata exceeded safe field limit: %w", domain.ErrUpstream)
	}
	return metadata, nil
}

func optionalDeploymentRevision(annotations map[string]string) (int, error) {
	value, exists := annotations[deploymentRevisionAnnotation]
	if !exists {
		return 0, nil
	}
	return canonicalDeploymentRevision(value)
}

func canonicalDeploymentRevision(value string) (int, error) {
	if value == "" || len(value) > 10 || value[0] == '0' {
		return 0, fmt.Errorf("invalid Deployment revision: %w", domain.ErrUpstream)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("invalid Deployment revision: %w", domain.ErrUpstream)
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf("invalid Deployment revision: %w", domain.ErrUpstream)
	}
	return int(parsed), nil
}

func boundedDeploymentRevisionHistory(
	reference domain.WorkloadReference,
	currentRevision, unassigned int,
	revisions []domain.DeploymentRevision,
) domain.DeploymentRevisionHistory {
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision > revisions[j].Revision })
	truncated := len(revisions) > deploymentRevisionMaxResults
	if truncated {
		revisions = revisions[:deploymentRevisionMaxResults]
	}
	result := make([]domain.DeploymentRevision, len(revisions))
	copy(result, revisions)
	return domain.DeploymentRevisionHistory{
		Namespace: reference.Namespace, Name: reference.Name, CurrentRevision: currentRevision,
		UnassignedReplicaSetCount: unassigned, Revisions: result, Truncated: truncated,
	}
}

func validDeploymentRevisionUID(value string) bool {
	return value != "" && len(value) <= deploymentRevisionMaxUIDBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validDeploymentRevisionOwner(apiVersion, kind, name, uid string) bool {
	return apiVersion != "" && len(apiVersion) <= 64 && apiVersion == strings.TrimSpace(apiVersion) &&
		kind != "" && len(kind) <= 64 && kind == strings.TrimSpace(kind) &&
		validKubernetesMetadataString(name) && validDeploymentRevisionUID(uid) &&
		strings.IndexFunc(apiVersion, unicode.IsControl) < 0 && strings.IndexFunc(kind, unicode.IsControl) < 0
}

func validDeploymentRevisionContinuation(value string) bool {
	return value != "" && len(value) <= deploymentRevisionMaxContinueBytes &&
		value == strings.TrimSpace(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}
