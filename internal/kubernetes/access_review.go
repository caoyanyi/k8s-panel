package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func (c *Client) ReviewServiceAccountAccess(
	ctx context.Context,
	input domain.KubernetesServiceAccountAccessReviewInput,
) (domain.KubernetesCapabilityState, error) {
	if err := domain.ValidateServiceAccountAccessReviewInput(input); err != nil {
		return "", err
	}
	requestBody := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Spec       struct {
			User               string                              `json:"user"`
			Groups             []string                            `json:"groups"`
			ResourceAttributes domain.KubernetesResourceAttributes `json:"resourceAttributes"`
		} `json:"spec"`
	}{APIVersion: "authorization.k8s.io/v1", Kind: "SubjectAccessReview"}
	serviceAccount := input.ServiceAccount
	requestBody.Spec.User = "system:serviceaccount:" + serviceAccount.Namespace + ":" + serviceAccount.Name
	requestBody.Spec.Groups = []string{
		"system:serviceaccounts",
		"system:serviceaccounts:" + serviceAccount.Namespace,
		"system:authenticated",
	}
	requestBody.Spec.ResourceAttributes = input.ResourceAttributes
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("encode Kubernetes service account access review: %w", err)
	}
	payload, _, err := c.requestPayload(
		ctx,
		http.MethodPost,
		"/apis/authorization.k8s.io/v1/subjectaccessreviews",
		nil,
		"application/json",
		"application/json",
		body,
		maxCapabilityReviewBytes,
		false,
	)
	if err != nil {
		return "", err
	}
	var response struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Status     struct {
			Allowed *bool `json:"allowed"`
			Denied  bool  `json:"denied"`
		} `json:"status"`
	}
	if err := json.Unmarshal(payload, &response); err != nil || response.APIVersion != "authorization.k8s.io/v1" ||
		response.Kind != "SubjectAccessReview" || response.Status.Allowed == nil ||
		(*response.Status.Allowed && response.Status.Denied) {
		return "", fmt.Errorf("decode Kubernetes service account access review: %w", domain.ErrUpstream)
	}
	if *response.Status.Allowed {
		return domain.KubernetesCapabilityAllowed, nil
	}
	if response.Status.Denied {
		return domain.KubernetesCapabilityDenied, nil
	}
	return domain.KubernetesCapabilityIndeterminate, nil
}
