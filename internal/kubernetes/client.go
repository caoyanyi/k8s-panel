package kubernetes

import (
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
	"strings"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/outbound"
)

const (
	maxResponseBytes = 8 * 1024 * 1024
	listPageSize     = "500"
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
	var version struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := c.getJSON(ctx, "/version", nil, &version); err != nil {
		return domain.ClusterProbe{}, err
	}
	namespaces, err := c.Namespaces(ctx)
	if err != nil {
		return domain.ClusterProbe{}, err
	}
	nodes, err := c.listRaw(ctx, "/api/v1/nodes", nil)
	if err != nil {
		return domain.ClusterProbe{}, err
	}
	return domain.ClusterProbe{
		Version:        version.GitVersion,
		NamespaceCount: len(namespaces),
		NodeCount:      len(nodes),
	}, nil
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
			Status   struct {
				Phase string `json:"phase"`
			} `json:"status"`
		}
		if err := json.Unmarshal(item, &resource); err != nil {
			return nil, fmt.Errorf("decode namespace: %w", err)
		}
		namespaces = append(namespaces, domain.Namespace{
			Name:      resource.Metadata.Name,
			Status:    resource.Status.Phase,
			CreatedAt: resource.Metadata.CreationTimestamp,
		})
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Name < namespaces[j].Name })
	return namespaces, nil
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

func (c *Client) Summary(ctx context.Context) (domain.ClusterSummary, error) {
	probe, err := c.Probe(ctx)
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
	for page := 0; page < 100; page++ {
		var response struct {
			Metadata struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
			Items []json.RawMessage `json:"items"`
		}
		if err := c.getJSON(ctx, path, query, &response); err != nil {
			return nil, err
		}
		items = append(items, response.Items...)
		if response.Metadata.Continue == "" {
			return items, nil
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, errors.New("Kubernetes list exceeded pagination limit")
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create Kubernetes request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return fmt.Errorf("Kubernetes request: %w", domain.ErrTimeout)
		}
		return fmt.Errorf("Kubernetes request: %w", domain.ErrUpstream)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("Kubernetes authentication rejected: %w", domain.ErrUnauthorized)
		case http.StatusForbidden:
			return fmt.Errorf("Kubernetes authorization rejected: %w", domain.ErrForbidden)
		case http.StatusNotFound:
			return fmt.Errorf("Kubernetes resource unavailable: %w", domain.ErrNotFound)
		default:
			return fmt.Errorf("Kubernetes returned HTTP %d: %w", response.StatusCode, domain.ErrUpstream)
		}
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read Kubernetes response: %w", domain.ErrUpstream)
	}
	if len(payload) > maxResponseBytes {
		return errors.New("Kubernetes response exceeded size limit")
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode Kubernetes response: %w", domain.ErrUpstream)
	}
	return nil
}

type objectMetadata struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
}

type containerSpec struct {
	Image string `json:"image"`
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
