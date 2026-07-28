package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	eventListPageSize           = "250"
	maxEventListPages           = 8
	maxEventListItems           = 2000
	maxEventListBytes     int64 = 16 * 1024 * 1024
	maxEventMessageBytes        = 1024
	maxEventScalarBytes         = 256
	maxContinueTokenBytes       = 16 * 1024
)

func (c *Client) Events(
	ctx context.Context,
	namespace, eventType string,
	limit int,
) ([]domain.KubernetesEvent, error) {
	if err := domain.ValidateKubernetesEventList(namespace, eventType, limit); err != nil {
		return nil, err
	}
	path := "/api/v1/events"
	if namespace != "" {
		path = "/api/v1/namespaces/" + namespace + "/events"
	}
	query := make(url.Values)
	if eventType != "" {
		query.Set("fieldSelector", "type="+eventType)
	}
	items, err := c.listEventRaw(ctx, path, query)
	if err != nil {
		return nil, err
	}
	events, err := decodeEvents(items, len(items))
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if event.Namespace == "" || domain.ValidateNamespace(event.Namespace) != nil || event.CreatedAt.IsZero() {
			return nil, fmt.Errorf("invalid Kubernetes event metadata: %w", domain.ErrUpstream)
		}
		if namespace != "" && event.Namespace != namespace {
			return nil, fmt.Errorf("Kubernetes event exceeded namespace scope: %w", domain.ErrUpstream)
		}
		if eventType != "" && event.Type != eventType {
			return nil, fmt.Errorf("Kubernetes event exceeded type scope: %w", domain.ErrUpstream)
		}
	}
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (c *Client) listEventRaw(ctx context.Context, path string, query url.Values) ([]json.RawMessage, error) {
	query = cloneValues(query)
	query.Set("limit", eventListPageSize)
	items := make([]json.RawMessage, 0)
	var totalBytes int64
	for page := 0; page < maxEventListPages; page++ {
		payload, _, err := c.getPayload(ctx, path, query, "application/json", maxResponseBytes, false)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > maxEventListBytes-totalBytes {
			return nil, fmt.Errorf("Kubernetes event list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		totalBytes += int64(len(payload))

		var response struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes event list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "v1" || response.Kind != "EventList" {
			return nil, fmt.Errorf("unsupported Kubernetes event list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > maxEventListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes event list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		items = append(items, response.Items...)
		if response.Metadata.Continue == "" {
			return items, nil
		}
		if !validContinueToken(response.Metadata.Continue) {
			return nil, fmt.Errorf("invalid Kubernetes event continuation token: %w", domain.ErrUpstream)
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes event list exceeded safe page limit: %w", domain.ErrUpstream)
}

func decodeEvents(items []json.RawMessage, limit int) ([]domain.KubernetesEvent, error) {
	events := make([]domain.KubernetesEvent, 0, min(limit, len(items)))
	for _, item := range items {
		var resource eventResource
		if err := json.Unmarshal(item, &resource); err != nil {
			return nil, fmt.Errorf("decode Kubernetes event: %w", domain.ErrUpstream)
		}
		event, err := projectEvent(resource)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	sort.SliceStable(events, func(i, j int) bool {
		left := firstNonZeroTime(events[i].LastSeen, events[i].CreatedAt)
		right := firstNonZeroTime(events[j].LastSeen, events[j].CreatedAt)
		if !left.Equal(right) {
			return left.After(right)
		}
		if events[i].Namespace != events[j].Namespace {
			return events[i].Namespace < events[j].Namespace
		}
		return events[i].Name < events[j].Name
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func projectEvent(resource eventResource) (domain.KubernetesEvent, error) {
	name, err := eventScalar(resource.Metadata.Name, false)
	if err != nil {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event name: %w", domain.ErrUpstream)
	}
	namespace, err := eventScalar(resource.Metadata.Namespace, true)
	if err != nil || (namespace != "" && domain.ValidateNamespace(namespace) != nil) {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event namespace: %w", domain.ErrUpstream)
	}
	eventType, err := eventScalar(resource.Type, true)
	if err != nil {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event type: %w", domain.ErrUpstream)
	}
	reason, err := eventScalar(resource.Reason, true)
	if err != nil {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event reason: %w", domain.ErrUpstream)
	}
	source, err := eventScalar(eventSource(resource), true)
	if err != nil {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event source: %w", domain.ErrUpstream)
	}
	objectKind, err := eventScalar(resource.InvolvedObject.Kind, true)
	if err != nil {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event object kind: %w", domain.ErrUpstream)
	}
	objectName, err := eventScalar(resource.InvolvedObject.Name, true)
	if err != nil {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event object name: %w", domain.ErrUpstream)
	}
	objectNamespace, err := eventScalar(resource.InvolvedObject.Namespace, true)
	if err != nil || (objectNamespace != "" && domain.ValidateNamespace(objectNamespace) != nil) {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event object namespace: %w", domain.ErrUpstream)
	}
	if namespace != "" && objectNamespace != "" && namespace != objectNamespace {
		return domain.KubernetesEvent{}, fmt.Errorf("Kubernetes event object exceeded namespace scope: %w", domain.ErrUpstream)
	}
	if resource.Count < 0 || resource.Series.Count < 0 {
		return domain.KubernetesEvent{}, fmt.Errorf("invalid Kubernetes event count: %w", domain.ErrUpstream)
	}
	message, truncated := boundedEventMessage(resource.Message)
	return domain.KubernetesEvent{
		Namespace: namespace, Name: name, Type: eventType, Reason: reason,
		Message: message, MessageTruncated: truncated, Source: source,
		ObjectKind: objectKind, ObjectName: objectName, Count: max(resource.Count, resource.Series.Count, 1),
		FirstSeen: firstNonZeroTime(resource.FirstTimestamp, resource.EventTime, resource.Metadata.CreationTimestamp),
		LastSeen:  firstNonZeroTime(resource.Series.LastObservedTime, resource.LastTimestamp, resource.EventTime, resource.Metadata.CreationTimestamp),
		CreatedAt: resource.Metadata.CreationTimestamp,
	}, nil
}

func eventScalar(value string, allowEmpty bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed != value || (!allowEmpty && value == "") || len(value) > maxEventScalarBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", domain.ErrUpstream
	}
	return value, nil
}

func boundedEventMessage(value string) (string, bool) {
	var result strings.Builder
	result.Grow(min(len(value), maxEventMessageBytes))
	pendingSpace := false
	truncated := false
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			pendingSpace = result.Len() > 0
			continue
		}
		required := utf8.RuneLen(character)
		if pendingSpace {
			required++
		}
		if result.Len()+required > maxEventMessageBytes {
			truncated = true
			break
		}
		if pendingSpace {
			result.WriteByte(' ')
			pendingSpace = false
		}
		result.WriteRune(character)
	}
	return result.String(), truncated
}

func validContinueToken(value string) bool {
	return value != "" && len(value) <= maxContinueTokenBytes &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
