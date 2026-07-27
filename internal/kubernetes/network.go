package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func (c *Client) Services(ctx context.Context, namespace string) ([]domain.KubernetesService, error) {
	path := "/api/v1/services"
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
		path = "/api/v1/namespaces/" + namespace + "/services"
	}
	items, err := c.listRaw(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	services := make([]domain.KubernetesService, 0, len(items))
	for _, item := range items {
		service, err := decodeService(item)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].Namespace != services[j].Namespace {
			return services[i].Namespace < services[j].Namespace
		}
		return services[i].Name < services[j].Name
	})
	return services, nil
}

func (c *Client) Ingresses(ctx context.Context, namespace string) ([]domain.KubernetesIngress, error) {
	path := "/apis/networking.k8s.io/v1/ingresses"
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
		path = "/apis/networking.k8s.io/v1/namespaces/" + namespace + "/ingresses"
	}
	items, err := c.listRaw(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	ingresses := make([]domain.KubernetesIngress, 0, len(items))
	for _, item := range items {
		ingress, err := decodeIngress(item)
		if err != nil {
			return nil, err
		}
		ingresses = append(ingresses, ingress)
	}
	sort.Slice(ingresses, func(i, j int) bool {
		if ingresses[i].Namespace != ingresses[j].Namespace {
			return ingresses[i].Namespace < ingresses[j].Namespace
		}
		return ingresses[i].Name < ingresses[j].Name
	})
	return ingresses, nil
}

func decodeService(item json.RawMessage) (domain.KubernetesService, error) {
	var resource struct {
		Metadata objectMetadata `json:"metadata"`
		Spec     struct {
			Type         string   `json:"type"`
			ClusterIP    string   `json:"clusterIP"`
			ExternalName string   `json:"externalName"`
			ExternalIPs  []string `json:"externalIPs"`
			Ports        []struct {
				Name       string          `json:"name"`
				Protocol   string          `json:"protocol"`
				Port       int32           `json:"port"`
				TargetPort json.RawMessage `json:"targetPort"`
				NodePort   int32           `json:"nodePort"`
			} `json:"ports"`
		} `json:"spec"`
		Status struct {
			LoadBalancer struct {
				Ingress []loadBalancerAddress `json:"ingress"`
			} `json:"loadBalancer"`
		} `json:"status"`
	}
	if err := json.Unmarshal(item, &resource); err != nil {
		return domain.KubernetesService{}, fmt.Errorf("decode service: %w", domain.ErrUpstream)
	}

	serviceType := resource.Spec.Type
	if serviceType == "" {
		serviceType = "ClusterIP"
	}
	service := domain.KubernetesService{
		Namespace:         resource.Metadata.Namespace,
		Name:              resource.Metadata.Name,
		Type:              serviceType,
		ClusterIP:         resource.Spec.ClusterIP,
		ExternalName:      resource.Spec.ExternalName,
		ExternalAddresses: make([]string, 0, min(len(resource.Spec.ExternalIPs), domain.MaxNetworkAddresses)),
		Ports:             make([]domain.ServicePort, 0, min(len(resource.Spec.Ports), domain.MaxServicePorts)),
		PortCount:         len(resource.Spec.Ports),
		CreatedAt:         resource.Metadata.CreationTimestamp,
	}
	for _, address := range resource.Spec.ExternalIPs {
		service.ExternalAddresses = appendBoundedNetworkValue(
			service.ExternalAddresses, &service.AddressCount, address, domain.MaxNetworkAddresses,
		)
	}
	for _, address := range resource.Status.LoadBalancer.Ingress {
		service.ExternalAddresses = appendBoundedNetworkValue(
			service.ExternalAddresses, &service.AddressCount, address.IP, domain.MaxNetworkAddresses,
		)
		service.ExternalAddresses = appendBoundedNetworkValue(
			service.ExternalAddresses, &service.AddressCount, address.Hostname, domain.MaxNetworkAddresses,
		)
	}
	for index := 0; index < len(resource.Spec.Ports) && index < domain.MaxServicePorts; index++ {
		port := resource.Spec.Ports[index]
		targetPort, err := decodeServiceTargetPort(port.TargetPort)
		if err != nil {
			return domain.KubernetesService{}, err
		}
		protocol := port.Protocol
		if protocol == "" {
			protocol = "TCP"
		}
		service.Ports = append(service.Ports, domain.ServicePort{
			Name: port.Name, Protocol: protocol, Port: port.Port, TargetPort: targetPort, NodePort: port.NodePort,
		})
	}
	return service, nil
}

func decodeIngress(item json.RawMessage) (domain.KubernetesIngress, error) {
	var resource struct {
		Metadata objectMetadata `json:"metadata"`
		Spec     struct {
			IngressClassName string            `json:"ingressClassName"`
			TLS              []json.RawMessage `json:"tls"`
			Rules            []struct {
				Host string `json:"host"`
				HTTP *struct {
					Paths []json.RawMessage `json:"paths"`
				} `json:"http"`
			} `json:"rules"`
		} `json:"spec"`
		Status struct {
			LoadBalancer struct {
				Ingress []loadBalancerAddress `json:"ingress"`
			} `json:"loadBalancer"`
		} `json:"status"`
	}
	if err := json.Unmarshal(item, &resource); err != nil {
		return domain.KubernetesIngress{}, fmt.Errorf("decode ingress: %w", domain.ErrUpstream)
	}
	ingress := domain.KubernetesIngress{
		Namespace: resource.Metadata.Namespace,
		Name:      resource.Metadata.Name,
		ClassName: resource.Spec.IngressClassName,
		Hosts:     make([]string, 0, min(len(resource.Spec.Rules), domain.MaxIngressHosts)),
		Addresses: make([]string, 0, min(len(resource.Status.LoadBalancer.Ingress), domain.MaxNetworkAddresses)),
		TLS:       len(resource.Spec.TLS) > 0,
		RuleCount: len(resource.Spec.Rules),
		CreatedAt: resource.Metadata.CreationTimestamp,
	}
	for _, rule := range resource.Spec.Rules {
		host := rule.Host
		if strings.TrimSpace(host) == "" {
			host = "*"
		}
		ingress.Hosts = appendBoundedNetworkValue(ingress.Hosts, &ingress.HostCount, host, domain.MaxIngressHosts)
		if rule.HTTP != nil {
			ingress.PathCount += len(rule.HTTP.Paths)
		}
	}
	for _, address := range resource.Status.LoadBalancer.Ingress {
		ingress.Addresses = appendBoundedNetworkValue(
			ingress.Addresses, &ingress.AddressCount, address.IP, domain.MaxNetworkAddresses,
		)
		ingress.Addresses = appendBoundedNetworkValue(
			ingress.Addresses, &ingress.AddressCount, address.Hostname, domain.MaxNetworkAddresses,
		)
	}
	return ingress, nil
}

type loadBalancerAddress struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

func decodeServiceTargetPort(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var named string
	if err := json.Unmarshal(raw, &named); err == nil {
		return named, nil
	}
	var numbered int32
	if err := json.Unmarshal(raw, &numbered); err == nil {
		return strconv.FormatInt(int64(numbered), 10), nil
	}
	return "", fmt.Errorf("decode service target port: %w", domain.ErrUpstream)
}

func appendBoundedNetworkValue(values []string, count *int, raw string, limit int) []string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	(*count)++
	if len(values) < limit {
		return append(values, value)
	}
	return values
}
