package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	endpointSliceServiceNameLabel      = "kubernetes.io/service-name"
	maxEndpointSliceEndpointsPerObject = 1000
	maxEndpointSlicePortsPerObject     = 100
	maxEndpointSliceNestedEntries      = 16 * 1024
)

type endpointSliceEndpointSource struct {
	Conditions struct {
		Ready       *bool `json:"ready"`
		Serving     *bool `json:"serving"`
		Terminating *bool `json:"terminating"`
	} `json:"conditions"`
}

type endpointSliceMetadataSource struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels"`
}

func (c *Client) EndpointSlices(ctx context.Context, namespace string) ([]domain.KubernetesEndpointSlice, error) {
	path := "/apis/discovery.k8s.io/v1/endpointslices"
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
		path = "/apis/discovery.k8s.io/v1/namespaces/" + namespace + "/endpointslices"
	}
	items, err := c.listGovernanceRaw(ctx, path, "discovery.k8s.io/v1", "EndpointSliceList")
	if err != nil {
		return nil, err
	}
	remaining := maxEndpointSliceNestedEntries
	slices := make([]domain.KubernetesEndpointSlice, 0, len(items))
	for _, item := range items {
		slice, err := decodeEndpointSlice(item, namespace, &remaining)
		if err != nil {
			return nil, err
		}
		slices = append(slices, slice)
	}
	sort.Slice(slices, func(i, j int) bool {
		if slices[i].Namespace != slices[j].Namespace {
			return slices[i].Namespace < slices[j].Namespace
		}
		return slices[i].Name < slices[j].Name
	})
	return slices, nil
}

func decodeEndpointSlice(
	raw json.RawMessage,
	expectedNamespace string,
	remainingEntries *int,
) (domain.KubernetesEndpointSlice, error) {
	var source struct {
		APIVersion  string                        `json:"apiVersion"`
		Kind        string                        `json:"kind"`
		Metadata    endpointSliceMetadataSource   `json:"metadata"`
		AddressType string                        `json:"addressType"`
		Endpoints   []endpointSliceEndpointSource `json:"endpoints"`
		Ports       []struct{}                    `json:"ports"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return domain.KubernetesEndpointSlice{}, fmt.Errorf("decode Kubernetes EndpointSlice: %w", domain.ErrUpstream)
	}
	serviceName := source.Metadata.Labels[endpointSliceServiceNameLabel]
	if err := validateEndpointSliceIdentity(
		source.APIVersion, source.Kind, source.Metadata, expectedNamespace, serviceName, source.AddressType,
	); err != nil {
		return domain.KubernetesEndpointSlice{}, err
	}
	if len(source.Endpoints) > maxEndpointSliceEndpointsPerObject || len(source.Ports) > maxEndpointSlicePortsPerObject {
		return domain.KubernetesEndpointSlice{}, fmt.Errorf("Kubernetes EndpointSlice exceeded safe entry limit: %w", domain.ErrUpstream)
	}
	nestedEntries := len(source.Endpoints) + len(source.Ports)
	if remainingEntries == nil || nestedEntries > *remainingEntries {
		return domain.KubernetesEndpointSlice{}, fmt.Errorf("Kubernetes EndpointSlice request exceeded safe nested entry limit: %w", domain.ErrUpstream)
	}
	*remainingEntries -= nestedEntries

	ready, readyDefaulted := 0, 0
	serving, servingDefaulted := 0, 0
	terminating, terminatingDefaulted := 0, 0
	for _, endpoint := range source.Endpoints {
		if endpoint.Conditions.Ready == nil {
			ready++
			readyDefaulted++
		} else if *endpoint.Conditions.Ready {
			ready++
		}
		if endpoint.Conditions.Serving == nil {
			serving++
			servingDefaulted++
		} else if *endpoint.Conditions.Serving {
			serving++
		}
		if endpoint.Conditions.Terminating == nil {
			terminatingDefaulted++
		} else if *endpoint.Conditions.Terminating {
			terminating++
		}
	}

	return domain.KubernetesEndpointSlice{
		Namespace: source.Metadata.Namespace, Name: source.Metadata.Name,
		ServiceName: serviceName, AddressType: source.AddressType,
		EndpointCount: len(source.Endpoints), PortCount: len(source.Ports),
		ReadyEndpointCount: ready, ReadyDefaultedCount: readyDefaulted,
		ServingEndpointCount: serving, ServingDefaultedCount: servingDefaulted,
		TerminatingEndpointCount: terminating, TerminatingDefaultedCount: terminatingDefaulted,
		CreatedAt: source.Metadata.CreationTimestamp,
	}, nil
}

func validateEndpointSliceIdentity(
	apiVersion, kind string,
	metadata endpointSliceMetadataSource,
	expectedNamespace, serviceName, addressType string,
) error {
	if apiVersion != "discovery.k8s.io/v1" || kind != "EndpointSlice" ||
		!validKubernetesMetadataString(metadata.Name) || metadata.CreationTimestamp.IsZero() ||
		domain.ValidateNamespace(metadata.Namespace) != nil || domain.ValidateNamespace(serviceName) != nil ||
		!validEndpointSliceAddressType(addressType) {
		return fmt.Errorf("invalid Kubernetes EndpointSlice object identity: %w", domain.ErrUpstream)
	}
	if expectedNamespace != "" && metadata.Namespace != expectedNamespace {
		return fmt.Errorf("Kubernetes EndpointSlice object exceeded namespace scope: %w", domain.ErrUpstream)
	}
	return nil
}

func validEndpointSliceAddressType(value string) bool {
	switch value {
	case "IPv4", "IPv6", "FQDN":
		return true
	default:
		return false
	}
}
