package kubernetes

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/outbound"
	"gopkg.in/yaml.v3"
)

const (
	maxResponseBytes  = 8 * 1024 * 1024
	maxMutationBytes  = 2 * 1024 * 1024
	maxLogBytes       = 2 * 1024 * 1024
	maxListBytes      = 32 * 1024 * 1024
	maxListItems      = 5000
	maxListPages      = 20
	listPageSize      = "500"
	restartAnnotation = "k8s-panel.io/restartedAt"
)

type Connection struct {
	Server      string
	CACert      string
	BearerToken string
}

type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

func NewClient(connection Connection, policy *outbound.Policy) (*Client, error) {
	if policy == nil {
		return nil, errors.New("outbound policy is required")
	}
	validationContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	baseURL, err := policy.ValidateHTTPSURL(validationContext, connection.Server)
	if err != nil {
		return nil, fmt.Errorf("validate Kubernetes server: %w", err)
	}
	if strings.TrimSpace(connection.BearerToken) == "" {
		return nil, errors.New("bearer token is required")
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if connection.CACert != "" && !rootCAs.AppendCertsFromPEM([]byte(connection.CACert)) {
		return nil, errors.New("CA certificate is not valid PEM")
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
		ServerName: baseURL.Hostname(),
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           policy.DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   4,
	}
	return &Client{
		baseURL: baseURL,
		token:   connection.BearerToken,
		http:    &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}, nil
}

func (c *Client) Probe(ctx context.Context) (domain.ClusterProbe, error) {
	probe, _, err := c.probeResources(ctx)
	return probe, err
}

func (c *Client) probeResources(ctx context.Context) (domain.ClusterProbe, []domain.Node, error) {
	var version struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := c.getJSON(ctx, "/version", nil, &version); err != nil {
		return domain.ClusterProbe{}, nil, err
	}
	namespaces, err := c.Namespaces(ctx)
	if err != nil {
		return domain.ClusterProbe{}, nil, err
	}
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return domain.ClusterProbe{}, nil, err
	}
	return domain.ClusterProbe{
		Version:        version.GitVersion,
		NamespaceCount: len(namespaces),
		NodeCount:      len(nodes),
	}, nodes, nil
}

func (c *Client) Namespaces(ctx context.Context) ([]domain.Namespace, error) {
	items, err := c.listRaw(ctx, "/api/v1/namespaces", nil)
	if err != nil {
		return nil, err
	}
	namespaces := make([]domain.Namespace, 0, len(items))
	for _, item := range items {
		var resource struct {
			Metadata objectMetadata `json:"metadata"`
			Spec     struct {
				Finalizers []string `json:"finalizers"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		}
		if err := json.Unmarshal(item, &resource); err != nil {
			return nil, fmt.Errorf("decode namespace: %w", err)
		}
		namespaces = append(namespaces, domain.Namespace{
			Name:       resource.Metadata.Name,
			Status:     resource.Status.Phase,
			Labels:     cloneStringMap(resource.Metadata.Labels),
			Finalizers: append(make([]string, 0, len(resource.Spec.Finalizers)), resource.Spec.Finalizers...),
			CreatedAt:  resource.Metadata.CreationTimestamp,
		})
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Name < namespaces[j].Name })
	return namespaces, nil
}

func (c *Client) Nodes(ctx context.Context) ([]domain.Node, error) {
	items, err := c.listRaw(ctx, "/api/v1/nodes", nil)
	if err != nil {
		return nil, err
	}
	nodes := make([]domain.Node, 0, len(items))
	for _, item := range items {
		node, _, err := decodeNode(item)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}

func (c *Client) NodeDetail(ctx context.Context, name string) (domain.NodeDetail, error) {
	if err := domain.ValidateNodeName(name); err != nil {
		return domain.NodeDetail{}, err
	}
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/api/v1/nodes/"+name, nil, &raw); err != nil {
		return domain.NodeDetail{}, err
	}
	node, resource, err := decodeNode(raw)
	if err != nil {
		return domain.NodeDetail{}, err
	}
	return domain.NodeDetail{
		Node:            node,
		UID:             resource.Metadata.UID,
		ResourceVersion: resource.Metadata.ResourceVersion,
		Labels:          cloneStringMap(resource.Metadata.Labels),
		Taints:          decodeNodeTaints(resource.Spec.Taints),
		Addresses:       decodeNodeAddresses(resource.Status.Addresses),
		Conditions:      decodeNodeConditions(resource.Status.Conditions),
		SystemInfo:      decodeNodeSystemInfo(resource.Status.NodeInfo),
	}, nil
}

func (c *Client) NodeEvents(ctx context.Context, name string, limit int) ([]domain.KubernetesEvent, error) {
	if err := domain.ValidateNodeName(name); err != nil {
		return nil, err
	}
	if limit < 1 || limit > domain.MaxNodeEventLimit {
		return nil, domain.Invalid("limit", "must be between 1 and 100")
	}
	query := make(url.Values)
	query.Set("fieldSelector", strings.Join([]string{
		"involvedObject.kind=Node",
		"involvedObject.name=" + name,
	}, ","))
	items, err := c.listRaw(ctx, "/api/v1/events", query)
	if err != nil {
		return nil, err
	}
	return decodeEvents(items, limit)
}

func (c *Client) Workloads(ctx context.Context, namespace, kind string) ([]domain.Workload, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "" && kind != "deployment" && kind != "statefulset" && kind != "daemonset" && kind != "pod" {
		return nil, domain.Invalid("kind", "must be deployment, statefulset, daemonset or pod")
	}
	paths := workloadPaths(namespace)
	workloads := make([]domain.Workload, 0)
	for _, resourceKind := range []string{"deployment", "statefulset", "daemonset", "pod"} {
		if kind != "" && kind != resourceKind {
			continue
		}
		items, err := c.listRaw(ctx, paths[resourceKind], nil)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			workload, err := decodeWorkload(resourceKind, item)
			if err != nil {
				return nil, err
			}
			workloads = append(workloads, workload)
		}
	}
	sort.Slice(workloads, func(i, j int) bool {
		if workloads[i].Namespace != workloads[j].Namespace {
			return workloads[i].Namespace < workloads[j].Namespace
		}
		if workloads[i].Kind != workloads[j].Kind {
			return workloads[i].Kind < workloads[j].Kind
		}
		return workloads[i].Name < workloads[j].Name
	})
	return workloads, nil
}

func (c *Client) WorkloadDetail(ctx context.Context, reference domain.WorkloadReference) (domain.WorkloadDetail, error) {
	reference.Kind = strings.ToLower(strings.TrimSpace(reference.Kind))
	if err := domain.ValidateWorkloadReference(reference); err != nil {
		return domain.WorkloadDetail{}, err
	}
	var raw json.RawMessage
	if err := c.getJSON(ctx, workloadResourcePath(reference), nil, &raw); err != nil {
		return domain.WorkloadDetail{}, err
	}
	workload, err := decodeWorkload(reference.Kind, raw)
	if err != nil {
		return domain.WorkloadDetail{}, err
	}
	var resource workloadDetailResource
	if err := json.Unmarshal(raw, &resource); err != nil {
		return domain.WorkloadDetail{}, fmt.Errorf("decode Kubernetes workload detail: %w", domain.ErrUpstream)
	}
	sanitized, err := sanitizedWorkloadYAML(raw)
	if err != nil {
		return domain.WorkloadDetail{}, err
	}
	return domain.WorkloadDetail{
		Workload:        workload,
		UID:             resource.Metadata.UID,
		ResourceVersion: resource.Metadata.ResourceVersion,
		Labels:          cloneStringMap(resource.Metadata.Labels),
		Containers:      decodeContainers(reference.Kind, resource),
		Conditions:      decodeConditions(resource.Status.Conditions),
		YAML:            sanitized,
	}, nil
}

func (c *Client) ScaleWorkload(
	ctx context.Context,
	reference domain.WorkloadReference,
	resourceVersion string,
	replicas int32,
) (domain.Workload, error) {
	if err := validateDeploymentMutation(reference, resourceVersion); err != nil {
		return domain.Workload{}, err
	}
	if replicas < 0 || replicas > domain.MaxWorkloadReplicas {
		return domain.Workload{}, domain.Invalid("replicas", "must be between 0 and 1000")
	}
	return c.patchDeployment(ctx, reference, map[string]any{
		"metadata": map[string]any{"resourceVersion": strings.TrimSpace(resourceVersion)},
		"spec":     map[string]any{"replicas": replicas},
	})
}

func (c *Client) RestartWorkload(
	ctx context.Context,
	reference domain.WorkloadReference,
	resourceVersion string,
	restartedAt time.Time,
) (domain.Workload, error) {
	if err := validateDeploymentMutation(reference, resourceVersion); err != nil {
		return domain.Workload{}, err
	}
	if restartedAt.IsZero() {
		return domain.Workload{}, domain.Invalid("restarted_at", "is required")
	}
	return c.patchDeployment(ctx, reference, map[string]any{
		"metadata": map[string]any{"resourceVersion": strings.TrimSpace(resourceVersion)},
		"spec": map[string]any{"template": map[string]any{
			"metadata": map[string]any{"annotations": map[string]any{
				restartAnnotation: restartedAt.UTC().Format(time.RFC3339Nano),
			}},
		}},
	})
}

func (c *Client) PreviewWorkloadImage(
	ctx context.Context,
	change domain.WorkloadImageChange,
) (domain.WorkloadImagePreview, error) {
	change = normalizeWorkloadImageChange(change)
	payload, err := c.patchDeploymentImage(ctx, change, true)
	if err != nil {
		return domain.WorkloadImagePreview{}, err
	}
	image, found, err := deploymentContainerImage(payload, change.Container)
	if err != nil {
		return domain.WorkloadImagePreview{}, err
	}
	if !found || image != change.Image {
		return domain.WorkloadImagePreview{}, fmt.Errorf("Kubernetes dry-run returned an unexpected container image: %w", domain.ErrUpstream)
	}
	return domain.WorkloadImagePreview{
		Kind:            "Deployment",
		Namespace:       change.Reference.Namespace,
		Name:            change.Reference.Name,
		Container:       change.Container,
		ResourceVersion: change.ResourceVersion,
		Changes: []domain.WorkloadFieldChange{{
			Field:  "spec.template.spec.containers[name=" + change.Container + "].image",
			Before: change.CurrentImage,
			After:  change.Image,
		}},
	}, nil
}

func (c *Client) UpdateWorkloadImage(
	ctx context.Context,
	change domain.WorkloadImageChange,
) (domain.Workload, error) {
	change = normalizeWorkloadImageChange(change)
	payload, err := c.patchDeploymentImage(ctx, change, false)
	if err != nil {
		return domain.Workload{}, err
	}
	image, found, err := deploymentContainerImage(payload, change.Container)
	if err != nil {
		return domain.Workload{}, err
	}
	if !found || image != change.Image {
		return domain.Workload{}, fmt.Errorf("Kubernetes update returned an unexpected container image: %w", domain.ErrUpstream)
	}
	return decodeWorkload("deployment", payload)
}

func (c *Client) patchDeploymentImage(
	ctx context.Context,
	change domain.WorkloadImageChange,
	dryRun bool,
) ([]byte, error) {
	if err := domain.ValidateWorkloadImageChange(change); err != nil {
		return nil, err
	}
	path := workloadResourcePath(change.Reference)
	currentPayload, _, err := c.requestPayload(
		ctx, http.MethodGet, path, nil, "application/json", "", nil, maxMutationBytes, false,
	)
	if err != nil {
		return nil, err
	}
	var current workloadDetailResource
	if err := json.Unmarshal(currentPayload, &current); err != nil {
		return nil, fmt.Errorf("decode Kubernetes Deployment for image update: %w", domain.ErrUpstream)
	}
	if current.Metadata.ResourceVersion != change.ResourceVersion {
		return nil, fmt.Errorf("Deployment resource version changed: %w", domain.ErrConflict)
	}
	currentImage, found := containerImage(current.Spec.Template.Spec.Containers, change.Container)
	if !found || currentImage != change.CurrentImage {
		return nil, fmt.Errorf("Deployment container image changed: %w", domain.ErrConflict)
	}
	patch := map[string]any{
		"metadata": map[string]any{"resourceVersion": change.ResourceVersion},
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []map[string]string{{"name": change.Container, "image": change.Image}},
		}}},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("encode Kubernetes image patch: %w", err)
	}
	var query url.Values
	if dryRun {
		query = make(url.Values)
		query.Set("dryRun", "All")
	}
	payload, _, err := c.requestPayload(
		ctx, http.MethodPatch, path, query, "application/json", "application/strategic-merge-patch+json",
		body, maxMutationBytes, false,
	)
	return payload, err
}

func normalizeWorkloadImageChange(change domain.WorkloadImageChange) domain.WorkloadImageChange {
	change.Reference.Kind = strings.ToLower(strings.TrimSpace(change.Reference.Kind))
	change.ResourceVersion = strings.TrimSpace(change.ResourceVersion)
	return change
}

func deploymentContainerImage(payload []byte, name string) (string, bool, error) {
	var resource workloadDetailResource
	if err := json.Unmarshal(payload, &resource); err != nil {
		return "", false, fmt.Errorf("decode Kubernetes Deployment image: %w", domain.ErrUpstream)
	}
	image, found := containerImage(resource.Spec.Template.Spec.Containers, name)
	return image, found, nil
}

func containerImage(containers []containerSpec, name string) (string, bool) {
	for _, container := range containers {
		if container.Name == name {
			return container.Image, true
		}
	}
	return "", false
}

func (c *Client) patchDeployment(
	ctx context.Context,
	reference domain.WorkloadReference,
	patch map[string]any,
) (domain.Workload, error) {
	body, err := json.Marshal(patch)
	if err != nil {
		return domain.Workload{}, fmt.Errorf("encode Kubernetes patch: %w", err)
	}
	path := "/apis/apps/v1/namespaces/" + reference.Namespace + "/deployments/" + reference.Name
	payload, _, err := c.requestPayload(
		ctx, http.MethodPatch, path, nil, "application/json", "application/merge-patch+json", body, maxResponseBytes, false,
	)
	if err != nil {
		return domain.Workload{}, err
	}
	workload, err := decodeWorkload("deployment", payload)
	if err != nil {
		return domain.Workload{}, err
	}
	return workload, nil
}

func validateDeploymentMutation(reference domain.WorkloadReference, resourceVersion string) error {
	if err := domain.ValidateWorkloadReference(reference); err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(reference.Kind)) != "deployment" {
		return domain.Invalid("kind", "only Deployment operations are supported")
	}
	if strings.TrimSpace(resourceVersion) == "" || len(strings.TrimSpace(resourceVersion)) > 256 {
		return domain.Invalid("resource_version", "is required and must not exceed 256 characters")
	}
	return nil
}

func (c *Client) WorkloadEvents(
	ctx context.Context,
	reference domain.WorkloadReference,
	limit int,
) ([]domain.KubernetesEvent, error) {
	reference.Kind = strings.ToLower(strings.TrimSpace(reference.Kind))
	if err := domain.ValidateWorkloadReference(reference); err != nil {
		return nil, err
	}
	if limit < 1 || limit > domain.MaxWorkloadEventLimit {
		return nil, domain.Invalid("limit", "must be between 1 and 100")
	}
	query := make(url.Values)
	query.Set("fieldSelector", strings.Join([]string{
		"involvedObject.kind=" + displayWorkloadKind(reference.Kind),
		"involvedObject.name=" + reference.Name,
	}, ","))
	items, err := c.listRaw(ctx, "/api/v1/namespaces/"+reference.Namespace+"/events", query)
	if err != nil {
		return nil, err
	}
	return decodeEvents(items, limit)
}

func decodeEvents(items []json.RawMessage, limit int) ([]domain.KubernetesEvent, error) {
	events := make([]domain.KubernetesEvent, 0, min(limit, len(items)))
	for _, item := range items {
		var resource eventResource
		if err := json.Unmarshal(item, &resource); err != nil {
			return nil, fmt.Errorf("decode Kubernetes event: %w", domain.ErrUpstream)
		}
		events = append(events, domain.KubernetesEvent{
			Name:      resource.Metadata.Name,
			Type:      resource.Type,
			Reason:    resource.Reason,
			Message:   resource.Message,
			Source:    eventSource(resource),
			Count:     max(resource.Count, 1),
			FirstSeen: firstNonZeroTime(resource.FirstTimestamp, resource.EventTime, resource.Metadata.CreationTimestamp),
			LastSeen:  firstNonZeroTime(resource.Series.LastObservedTime, resource.LastTimestamp, resource.EventTime, resource.Metadata.CreationTimestamp),
		})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].LastSeen.After(events[j].LastSeen) })
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (c *Client) PodLogs(ctx context.Context, input domain.PodLogRequest) (domain.PodLogs, error) {
	if err := domain.ValidatePodLogRequest(input); err != nil {
		return domain.PodLogs{}, err
	}
	query := make(url.Values)
	query.Set("container", input.Container)
	query.Set("tailLines", strconv.Itoa(input.TailLines))
	query.Set("previous", strconv.FormatBool(input.Previous))
	query.Set("timestamps", strconv.FormatBool(input.Timestamps))
	path := "/api/v1/namespaces/" + input.Namespace + "/pods/" + input.Pod + "/log"
	payload, truncated, err := c.getPayload(ctx, path, query, "text/plain", maxLogBytes, true)
	if err != nil {
		return domain.PodLogs{}, err
	}
	payload = bytes.ToValidUTF8(payload, []byte("\uFFFD"))
	return domain.PodLogs{
		Namespace: input.Namespace, Pod: input.Pod, Container: input.Container, TailLines: input.TailLines,
		Previous: input.Previous, Timestamps: input.Timestamps, Truncated: truncated, Content: string(payload),
	}, nil
}

func (c *Client) Summary(ctx context.Context) (domain.ClusterSummary, error) {
	probe, nodes, err := c.probeResources(ctx)
	if err != nil {
		return domain.ClusterSummary{}, err
	}
	workloads, err := c.Workloads(ctx, "", "")
	if err != nil {
		return domain.ClusterSummary{}, err
	}
	summary := domain.ClusterSummary{
		Version:        probe.Version,
		NamespaceCount: probe.NamespaceCount,
		NodeCount:      probe.NodeCount,
	}
	for _, node := range nodes {
		if node.Status == "Ready" {
			summary.ReadyNodeCount++
		}
	}
	for _, workload := range workloads {
		if workload.Kind == "Pod" {
			if workload.Status != "Ready" && workload.Status != "Succeeded" {
				summary.UnhealthyPods++
			}
			continue
		}
		summary.WorkloadCount++
		if workload.Ready == workload.Desired {
			summary.ReadyWorkloads++
		}
	}
	return summary, nil
}

func (c *Client) listRaw(ctx context.Context, path string, query url.Values) ([]json.RawMessage, error) {
	if query == nil {
		query = make(url.Values)
	} else {
		query = cloneValues(query)
	}
	query.Set("limit", listPageSize)
	items := make([]json.RawMessage, 0)
	var totalBytes int64
	for page := 0; page < maxListPages; page++ {
		var response struct {
			Metadata struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
			Items []json.RawMessage `json:"items"`
		}
		if err := c.getJSON(ctx, path, query, &response); err != nil {
			return nil, err
		}
		var appendErr error
		items, totalBytes, appendErr = appendListItems(items, totalBytes, response.Items)
		if appendErr != nil {
			return nil, appendErr
		}
		if response.Metadata.Continue == "" {
			return items, nil
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes list exceeded safe page limit: %w", domain.ErrUpstream)
}

func appendListItems(
	items []json.RawMessage,
	totalBytes int64,
	pageItems []json.RawMessage,
) ([]json.RawMessage, int64, error) {
	if len(pageItems) > maxListItems-len(items) {
		return nil, totalBytes, fmt.Errorf("Kubernetes list exceeded safe item limit: %w", domain.ErrUpstream)
	}
	for _, item := range pageItems {
		if int64(len(item)) > maxListBytes-totalBytes {
			return nil, totalBytes, fmt.Errorf("Kubernetes list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		totalBytes += int64(len(item))
	}
	return append(items, pageItems...), totalBytes, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	payload, _, err := c.getPayload(ctx, path, query, "application/json", maxResponseBytes, false)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode Kubernetes response: %w", domain.ErrUpstream)
	}
	return nil
}

func (c *Client) getPayload(
	ctx context.Context,
	path string,
	query url.Values,
	accept string,
	maxBytes int64,
	truncate bool,
) ([]byte, bool, error) {
	return c.requestPayload(ctx, http.MethodGet, path, query, accept, "", nil, maxBytes, truncate)
}

func (c *Client) requestPayload(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	accept string,
	contentType string,
	body []byte,
	maxBytes int64,
	truncate bool,
) ([]byte, bool, error) {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	if query != nil {
		requestURL.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("create Kubernetes request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", accept)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, false, fmt.Errorf("Kubernetes request canceled: %w", context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, false, fmt.Errorf("Kubernetes request: %w", domain.ErrTimeout)
		}
		return nil, false, fmt.Errorf("Kubernetes request: %w", domain.ErrUpstream)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return nil, false, fmt.Errorf("Kubernetes authentication rejected: %w", domain.ErrUnauthorized)
		case http.StatusForbidden:
			return nil, false, fmt.Errorf("Kubernetes authorization rejected: %w", domain.ErrForbidden)
		case http.StatusNotFound:
			return nil, false, fmt.Errorf("Kubernetes resource unavailable: %w", domain.ErrNotFound)
		case http.StatusConflict:
			return nil, false, fmt.Errorf("Kubernetes resource version conflict: %w", domain.ErrConflict)
		default:
			return nil, false, fmt.Errorf("Kubernetes returned HTTP %d: %w", response.StatusCode, domain.ErrUpstream)
		}
	}
	limited := io.LimitReader(response.Body, maxBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, fmt.Errorf("read Kubernetes response: %w", domain.ErrUpstream)
	}
	if int64(len(payload)) > maxBytes {
		if truncate {
			return payload[:maxBytes], true, nil
		}
		return nil, false, fmt.Errorf("Kubernetes response exceeded size limit: %w", domain.ErrUpstream)
	}
	return payload, false, nil
}

type objectMetadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	ResourceVersion   string            `json:"resourceVersion"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
}

type containerSpec struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type containerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        struct {
		Waiting *struct {
			Reason string `json:"reason"`
		} `json:"waiting"`
		Running    *struct{} `json:"running"`
		Terminated *struct {
			Reason string `json:"reason"`
		} `json:"terminated"`
	} `json:"state"`
}

type workloadCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	Message            string    `json:"message"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

type nodeTaint struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Effect    string    `json:"effect"`
	TimeAdded time.Time `json:"timeAdded"`
}

type nodeAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type nodeSystemInfo struct {
	OSImage                 string `json:"osImage"`
	KernelVersion           string `json:"kernelVersion"`
	ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
	KubeletVersion          string `json:"kubeletVersion"`
	OperatingSystem         string `json:"operatingSystem"`
	Architecture            string `json:"architecture"`
}

type nodeCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	Message            string    `json:"message"`
	LastHeartbeatTime  time.Time `json:"lastHeartbeatTime"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

type nodeResource struct {
	Metadata objectMetadata `json:"metadata"`
	Spec     struct {
		Unschedulable bool        `json:"unschedulable"`
		Taints        []nodeTaint `json:"taints"`
	} `json:"spec"`
	Status struct {
		Capacity    map[string]string `json:"capacity"`
		Allocatable map[string]string `json:"allocatable"`
		Addresses   []nodeAddress     `json:"addresses"`
		Conditions  []nodeCondition   `json:"conditions"`
		NodeInfo    nodeSystemInfo    `json:"nodeInfo"`
	} `json:"status"`
}

type workloadDetailResource struct {
	Metadata objectMetadata `json:"metadata"`
	Spec     struct {
		Containers          []containerSpec `json:"containers"`
		InitContainers      []containerSpec `json:"initContainers"`
		EphemeralContainers []containerSpec `json:"ephemeralContainers"`
		Template            struct {
			Spec struct {
				Containers     []containerSpec `json:"containers"`
				InitContainers []containerSpec `json:"initContainers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Conditions                 []workloadCondition `json:"conditions"`
		ContainerStatuses          []containerStatus   `json:"containerStatuses"`
		InitContainerStatuses      []containerStatus   `json:"initContainerStatuses"`
		EphemeralContainerStatuses []containerStatus   `json:"ephemeralContainerStatuses"`
	} `json:"status"`
}

type eventResource struct {
	Metadata           objectMetadata `json:"metadata"`
	Type               string         `json:"type"`
	Reason             string         `json:"reason"`
	Message            string         `json:"message"`
	Count              int32          `json:"count"`
	ReportingComponent string         `json:"reportingComponent"`
	Source             struct {
		Component string `json:"component"`
	} `json:"source"`
	EventTime      time.Time `json:"eventTime"`
	FirstTimestamp time.Time `json:"firstTimestamp"`
	LastTimestamp  time.Time `json:"lastTimestamp"`
	Series         struct {
		LastObservedTime time.Time `json:"lastObservedTime"`
	} `json:"series"`
}

func workloadResourcePath(reference domain.WorkloadReference) string {
	base := "/api/v1/namespaces/" + reference.Namespace + "/pods/"
	switch reference.Kind {
	case "deployment":
		base = "/apis/apps/v1/namespaces/" + reference.Namespace + "/deployments/"
	case "statefulset":
		base = "/apis/apps/v1/namespaces/" + reference.Namespace + "/statefulsets/"
	case "daemonset":
		base = "/apis/apps/v1/namespaces/" + reference.Namespace + "/daemonsets/"
	}
	return base + reference.Name
}

func displayWorkloadKind(kind string) string {
	switch kind {
	case "deployment":
		return "Deployment"
	case "statefulset":
		return "StatefulSet"
	case "daemonset":
		return "DaemonSet"
	default:
		return "Pod"
	}
}

func decodeContainers(kind string, resource workloadDetailResource) []domain.WorkloadContainer {
	if kind != "pod" {
		return mergeContainerDetails(resource.Spec.Template.Spec.Containers, nil, "container")
	}
	containers := mergeContainerDetails(resource.Spec.Containers, resource.Status.ContainerStatuses, "container")
	containers = append(containers, mergeContainerDetails(resource.Spec.InitContainers, resource.Status.InitContainerStatuses, "init")...)
	containers = append(containers, mergeContainerDetails(resource.Spec.EphemeralContainers, resource.Status.EphemeralContainerStatuses, "ephemeral")...)
	return containers
}

func mergeContainerDetails(specs []containerSpec, statuses []containerStatus, containerType string) []domain.WorkloadContainer {
	byName := make(map[string]containerStatus, len(statuses))
	for _, status := range statuses {
		byName[status.Name] = status
	}
	containers := make([]domain.WorkloadContainer, 0, len(specs))
	for _, spec := range specs {
		status := byName[spec.Name]
		containers = append(containers, domain.WorkloadContainer{
			Name: spec.Name, Image: spec.Image, Type: containerType, Ready: status.Ready,
			RestartCount: status.RestartCount, State: containerState(status),
		})
	}
	return containers
}

func containerState(status containerStatus) string {
	if status.State.Running != nil {
		return "Running"
	}
	if status.State.Waiting != nil {
		if status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
		return "Waiting"
	}
	if status.State.Terminated != nil {
		if status.State.Terminated.Reason != "" {
			return status.State.Terminated.Reason
		}
		return "Terminated"
	}
	return ""
}

func decodeConditions(conditions []workloadCondition) []domain.WorkloadCondition {
	result := make([]domain.WorkloadCondition, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, domain.WorkloadCondition{
			Type: condition.Type, Status: condition.Status, Reason: condition.Reason,
			Message: condition.Message, LastTransitionTime: condition.LastTransitionTime,
		})
	}
	return result
}

func sanitizedWorkloadYAML(raw json.RawMessage) (string, error) {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", fmt.Errorf("decode workload for YAML: %w", domain.ErrUpstream)
	}
	delete(object, "status")
	if metadata, ok := object["metadata"].(map[string]any); ok {
		for _, key := range []string{
			"annotations", "creationTimestamp", "generation", "managedFields", "resourceVersion", "selfLink", "uid",
		} {
			delete(metadata, key)
		}
	}
	redactContainerEnvironment(object)
	redactSensitiveFields(object)
	payload, err := yaml.Marshal(object)
	if err != nil {
		return "", fmt.Errorf("encode workload YAML: %w", err)
	}
	return string(payload), nil
}

func redactContainerEnvironment(object map[string]any) {
	spec, _ := object["spec"].(map[string]any)
	redactSpecEnvironment(spec)
	if template, ok := spec["template"].(map[string]any); ok {
		if templateSpec, ok := template["spec"].(map[string]any); ok {
			redactSpecEnvironment(templateSpec)
		}
	}
}

func redactSpecEnvironment(spec map[string]any) {
	for _, containerKey := range []string{"containers", "initContainers", "ephemeralContainers"} {
		containers, _ := spec[containerKey].([]any)
		for _, rawContainer := range containers {
			container, _ := rawContainer.(map[string]any)
			environment, _ := container["env"].([]any)
			for _, rawVariable := range environment {
				variable, _ := rawVariable.(map[string]any)
				if _, exists := variable["value"]; exists {
					variable["value"] = "<redacted>"
				}
			}
		}
	}
}

func redactSensitiveFields(value any) {
	sensitive := map[string]struct{}{
		"apikey": {}, "authorization": {}, "clientsecret": {}, "credentials": {},
		"password": {}, "passwd": {}, "privatekey": {}, "token": {},
	}
	sensitiveSequences := map[string]struct{}{"args": {}, "command": {}}
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(key))
				if _, found := sensitiveSequences[normalized]; found {
					typed[key] = []any{"<redacted>"}
					continue
				}
				if _, found := sensitive[normalized]; found {
					typed[key] = "<redacted>"
					continue
				}
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(value)
}

func cloneStringMap(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func eventSource(resource eventResource) string {
	if resource.ReportingComponent != "" {
		return resource.ReportingComponent
	}
	return resource.Source.Component
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func decodeWorkload(kind string, item json.RawMessage) (domain.Workload, error) {
	if kind == "pod" {
		return decodePod(item)
	}
	var resource struct {
		Metadata objectMetadata `json:"metadata"`
		Spec     struct {
			Replicas *int32 `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []containerSpec `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas          int32 `json:"readyReplicas"`
			AvailableReplicas      int32 `json:"availableReplicas"`
			DesiredNumberScheduled int32 `json:"desiredNumberScheduled"`
			NumberReady            int32 `json:"numberReady"`
		} `json:"status"`
	}
	if err := json.Unmarshal(item, &resource); err != nil {
		return domain.Workload{}, fmt.Errorf("decode Kubernetes workload: %w", err)
	}
	desired := int32(1)
	ready := resource.Status.ReadyReplicas
	displayKind := "Deployment"
	if resource.Spec.Replicas != nil {
		desired = *resource.Spec.Replicas
	}
	if kind == "statefulset" {
		displayKind = "StatefulSet"
	}
	if kind == "daemonset" {
		displayKind = "DaemonSet"
		desired = resource.Status.DesiredNumberScheduled
		ready = resource.Status.NumberReady
	}
	images := make([]string, 0, len(resource.Spec.Template.Spec.Containers))
	for _, container := range resource.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}
	return domain.Workload{
		Kind:      displayKind,
		Namespace: resource.Metadata.Namespace,
		Name:      resource.Metadata.Name,
		Ready:     ready,
		Desired:   desired,
		Status:    replicaStatus(ready, desired),
		Images:    images,
		CreatedAt: resource.Metadata.CreationTimestamp,
	}, nil
}

func decodeNode(item json.RawMessage) (domain.Node, nodeResource, error) {
	var resource nodeResource
	if err := json.Unmarshal(item, &resource); err != nil {
		return domain.Node{}, nodeResource{}, fmt.Errorf("decode Kubernetes node: %w", domain.ErrUpstream)
	}
	return domain.Node{
		Name:          resource.Metadata.Name,
		Status:        nodeReadyStatus(resource.Status.Conditions),
		Roles:         nodeRoles(resource.Metadata.Labels),
		Version:       resource.Status.NodeInfo.KubeletVersion,
		InternalIP:    nodeInternalIP(resource.Status.Addresses),
		OSImage:       resource.Status.NodeInfo.OSImage,
		Architecture:  resource.Status.NodeInfo.Architecture,
		Capacity:      decodeNodeResources(resource.Status.Capacity),
		Allocatable:   decodeNodeResources(resource.Status.Allocatable),
		Unschedulable: resource.Spec.Unschedulable,
		TaintCount:    len(resource.Spec.Taints),
		CreatedAt:     resource.Metadata.CreationTimestamp,
	}, resource, nil
}

func nodeReadyStatus(conditions []nodeCondition) string {
	for _, condition := range conditions {
		if condition.Type != "Ready" {
			continue
		}
		switch condition.Status {
		case "True":
			return "Ready"
		case "False":
			return "NotReady"
		default:
			return "Unknown"
		}
	}
	return "Unknown"
}

func nodeRoles(labels map[string]string) []string {
	const rolePrefix = "node-role.kubernetes.io/"
	roles := make(map[string]struct{})
	for key, value := range labels {
		role := ""
		switch {
		case strings.HasPrefix(key, rolePrefix):
			role = strings.TrimPrefix(key, rolePrefix)
			if role == "" {
				role = value
			}
		case key == "kubernetes.io/role":
			role = value
		}
		if role != "" {
			roles[role] = struct{}{}
		}
	}
	result := make([]string, 0, len(roles))
	for role := range roles {
		result = append(result, role)
	}
	sort.Strings(result)
	return result
}

func nodeInternalIP(addresses []nodeAddress) string {
	for _, address := range addresses {
		if address.Type == "InternalIP" {
			return address.Address
		}
	}
	return ""
}

func decodeNodeResources(resources map[string]string) domain.NodeResources {
	return domain.NodeResources{
		CPU:              resources["cpu"],
		Memory:           resources["memory"],
		Pods:             resources["pods"],
		EphemeralStorage: resources["ephemeral-storage"],
	}
}

func decodeNodeTaints(taints []nodeTaint) []domain.NodeTaint {
	result := make([]domain.NodeTaint, 0, len(taints))
	for _, taint := range taints {
		result = append(result, domain.NodeTaint{
			Key: taint.Key, Value: taint.Value, Effect: taint.Effect, TimeAdded: taint.TimeAdded,
		})
	}
	return result
}

func decodeNodeAddresses(addresses []nodeAddress) []domain.NodeAddress {
	result := make([]domain.NodeAddress, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, domain.NodeAddress{Type: address.Type, Address: address.Address})
	}
	return result
}

func decodeNodeConditions(conditions []nodeCondition) []domain.NodeCondition {
	result := make([]domain.NodeCondition, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, domain.NodeCondition{
			Type: condition.Type, Status: condition.Status, Reason: condition.Reason, Message: condition.Message,
			LastHeartbeatTime: condition.LastHeartbeatTime, LastTransitionTime: condition.LastTransitionTime,
		})
	}
	return result
}

func decodeNodeSystemInfo(info nodeSystemInfo) domain.NodeSystemInfo {
	return domain.NodeSystemInfo{
		OSImage: info.OSImage, KernelVersion: info.KernelVersion, ContainerRuntimeVersion: info.ContainerRuntimeVersion,
		KubeletVersion: info.KubeletVersion, OperatingSystem: info.OperatingSystem, Architecture: info.Architecture,
	}
}

func decodePod(item json.RawMessage) (domain.Workload, error) {
	var resource struct {
		Metadata objectMetadata `json:"metadata"`
		Spec     struct {
			Containers []containerSpec `json:"containers"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready bool `json:"ready"`
			} `json:"containerStatuses"`
		} `json:"status"`
	}
	if err := json.Unmarshal(item, &resource); err != nil {
		return domain.Workload{}, fmt.Errorf("decode Kubernetes pod: %w", err)
	}
	ready := int32(0)
	for _, status := range resource.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
	}
	desired := int32(len(resource.Spec.Containers))
	status := resource.Status.Phase
	if status == "Running" && ready == desired {
		status = "Ready"
	}
	images := make([]string, 0, len(resource.Spec.Containers))
	for _, container := range resource.Spec.Containers {
		images = append(images, container.Image)
	}
	return domain.Workload{
		Kind:      "Pod",
		Namespace: resource.Metadata.Namespace,
		Name:      resource.Metadata.Name,
		Ready:     ready,
		Desired:   desired,
		Status:    status,
		Images:    images,
		CreatedAt: resource.Metadata.CreationTimestamp,
	}, nil
}

func replicaStatus(ready, desired int32) string {
	if desired == ready {
		return "Ready"
	}
	if ready > 0 {
		return "Progressing"
	}
	return "Unavailable"
}

func workloadPaths(namespace string) map[string]string {
	if namespace == "" {
		return map[string]string{
			"deployment":  "/apis/apps/v1/deployments",
			"statefulset": "/apis/apps/v1/statefulsets",
			"daemonset":   "/apis/apps/v1/daemonsets",
			"pod":         "/api/v1/pods",
		}
	}
	escaped := url.PathEscape(namespace)
	return map[string]string{
		"deployment":  "/apis/apps/v1/namespaces/" + escaped + "/deployments",
		"statefulset": "/apis/apps/v1/namespaces/" + escaped + "/statefulsets",
		"daemonset":   "/apis/apps/v1/namespaces/" + escaped + "/daemonsets",
		"pod":         "/api/v1/namespaces/" + escaped + "/pods",
	}
}

func cloneValues(source url.Values) url.Values {
	cloned := make(url.Values, len(source))
	for key, values := range source {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}
