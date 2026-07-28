package kubernetes

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func decodeJob(item json.RawMessage) (domain.Workload, error) {
	var resource struct {
		Metadata objectMetadata `json:"metadata"`
		Spec     struct {
			Completions *int32 `json:"completions"`
			Suspend     bool   `json:"suspend"`
			Template    struct {
				Spec struct {
					Containers []containerSpec `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			Active     int32               `json:"active"`
			Succeeded  int32               `json:"succeeded"`
			Failed     int32               `json:"failed"`
			Conditions []workloadCondition `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal(item, &resource); err != nil {
		return domain.Workload{}, fmt.Errorf("decode Kubernetes Job: %w", domain.ErrUpstream)
	}
	desired := int32(1)
	if resource.Spec.Completions != nil {
		desired = *resource.Spec.Completions
	}
	images := make([]string, 0, len(resource.Spec.Template.Spec.Containers))
	for _, container := range resource.Spec.Template.Spec.Containers {
		images = append(images, container.Image)
	}
	return domain.Workload{
		Kind: "Job", Namespace: resource.Metadata.Namespace, Name: resource.Metadata.Name,
		Ready: resource.Status.Succeeded, Desired: desired,
		Status: jobStatus(resource.Spec.Suspend, resource.Status.Active, resource.Status.Succeeded, resource.Status.Failed, desired, resource.Status.Conditions),
		Images: images, CreatedAt: resource.Metadata.CreationTimestamp,
	}, nil
}

func jobStatus(suspended bool, active, succeeded, failed, desired int32, conditions []workloadCondition) string {
	for _, condition := range conditions {
		if condition.Status != "True" {
			continue
		}
		switch condition.Type {
		case "Complete":
			return "Succeeded"
		case "Failed":
			return "Failed"
		case "Suspended":
			return "Suspended"
		}
	}
	if suspended {
		return "Suspended"
	}
	if active > 0 {
		return "Running"
	}
	if desired > 0 && succeeded >= desired {
		return "Succeeded"
	}
	if failed > 0 {
		return "Retrying"
	}
	return "Pending"
}

func decodeCronJob(item json.RawMessage) (domain.Workload, error) {
	var resource struct {
		Metadata objectMetadata `json:"metadata"`
		Spec     struct {
			Suspend     bool `json:"suspend"`
			JobTemplate struct {
				Spec struct {
					Template struct {
						Spec struct {
							Containers []containerSpec `json:"containers"`
						} `json:"spec"`
					} `json:"template"`
				} `json:"spec"`
			} `json:"jobTemplate"`
		} `json:"spec"`
		Status struct {
			Active           []struct{} `json:"active"`
			LastScheduleTime time.Time  `json:"lastScheduleTime"`
		} `json:"status"`
	}
	if err := json.Unmarshal(item, &resource); err != nil {
		return domain.Workload{}, fmt.Errorf("decode Kubernetes CronJob: %w", domain.ErrUpstream)
	}
	containers := resource.Spec.JobTemplate.Spec.Template.Spec.Containers
	images := make([]string, 0, len(containers))
	for _, container := range containers {
		images = append(images, container.Image)
	}
	active := int32(len(resource.Status.Active))
	status := "Pending"
	if resource.Spec.Suspend {
		status = "Suspended"
	} else if active > 0 {
		status = "Running"
	} else if !resource.Status.LastScheduleTime.IsZero() {
		status = "Scheduled"
	}
	return domain.Workload{
		Kind: "CronJob", Namespace: resource.Metadata.Namespace, Name: resource.Metadata.Name,
		Ready: active, Desired: active, Status: status, Images: images, CreatedAt: resource.Metadata.CreationTimestamp,
	}, nil
}
