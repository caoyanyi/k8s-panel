package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	maxAvailabilityConditionsPerObject       = 8
	maxAvailabilityIntOrStringBytes          = 64
	defaultHPAMinReplicas              int32 = 1
	defaultPDBUnhealthyPolicy                = "IfHealthyBudget"
)

type availabilityConditionSource struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type availabilitySelectorSource struct {
	MatchLabels      map[string]json.RawMessage `json:"matchLabels"`
	MatchExpressions []json.RawMessage          `json:"matchExpressions"`
}

func (c *Client) HorizontalPodAutoscalers(
	ctx context.Context,
	namespace string,
) ([]domain.KubernetesHorizontalPodAutoscaler, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	items, err := c.listGovernanceRaw(
		ctx,
		"/apis/autoscaling/v2/namespaces/"+namespace+"/horizontalpodautoscalers",
		"autoscaling/v2",
		"HorizontalPodAutoscalerList",
	)
	if err != nil {
		return nil, err
	}
	remainingConditions := maxGovernanceProjectedEntries
	autoscalers := make([]domain.KubernetesHorizontalPodAutoscaler, 0, len(items))
	for _, item := range items {
		autoscaler, err := decodeHorizontalPodAutoscaler(item, namespace, &remainingConditions)
		if err != nil {
			return nil, err
		}
		autoscalers = append(autoscalers, autoscaler)
	}
	sort.Slice(autoscalers, func(i, j int) bool { return autoscalers[i].Name < autoscalers[j].Name })
	return autoscalers, nil
}

func (c *Client) PodDisruptionBudgets(
	ctx context.Context,
	namespace string,
) ([]domain.KubernetesPodDisruptionBudget, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	return c.listPodDisruptionBudgets(
		ctx,
		"/apis/policy/v1/namespaces/"+namespace+"/poddisruptionbudgets",
		namespace,
		false,
	)
}

func (c *Client) DisruptionBudgets(ctx context.Context) ([]domain.KubernetesPodDisruptionBudget, error) {
	return c.listPodDisruptionBudgets(ctx, "/apis/policy/v1/poddisruptionbudgets", "", true)
}

func (c *Client) listPodDisruptionBudgets(
	ctx context.Context,
	path, expectedNamespace string,
	allNamespaces bool,
) ([]domain.KubernetesPodDisruptionBudget, error) {
	items, err := c.listGovernanceRaw(
		ctx,
		path,
		"policy/v1",
		"PodDisruptionBudgetList",
	)
	if err != nil {
		return nil, err
	}
	remainingConditions := maxGovernanceProjectedEntries
	budgets := make([]domain.KubernetesPodDisruptionBudget, 0, len(items))
	for _, item := range items {
		budget, err := decodePodDisruptionBudget(item, expectedNamespace, allNamespaces, &remainingConditions)
		if err != nil {
			return nil, err
		}
		budgets = append(budgets, budget)
	}
	sort.Slice(budgets, func(i, j int) bool {
		if budgets[i].Namespace != budgets[j].Namespace {
			return budgets[i].Namespace < budgets[j].Namespace
		}
		return budgets[i].Name < budgets[j].Name
	})
	return budgets, nil
}

func decodeHorizontalPodAutoscaler(
	raw json.RawMessage,
	expectedNamespace string,
	remainingConditions *int,
) (domain.KubernetesHorizontalPodAutoscaler, error) {
	var source struct {
		APIVersion string             `json:"apiVersion"`
		Kind       string             `json:"kind"`
		Metadata   governanceMetadata `json:"metadata"`
		Spec       struct {
			ScaleTargetRef struct {
				APIVersion string `json:"apiVersion"`
				Kind       string `json:"kind"`
				Name       string `json:"name"`
			} `json:"scaleTargetRef"`
			MinReplicas *int32            `json:"minReplicas"`
			MaxReplicas int32             `json:"maxReplicas"`
			Metrics     []json.RawMessage `json:"metrics"`
		} `json:"spec"`
		Status struct {
			ObservedGeneration *int64                        `json:"observedGeneration"`
			CurrentReplicas    int32                         `json:"currentReplicas"`
			DesiredReplicas    int32                         `json:"desiredReplicas"`
			CurrentMetrics     []json.RawMessage             `json:"currentMetrics"`
			LastScaleTime      *time.Time                    `json:"lastScaleTime"`
			Conditions         []availabilityConditionSource `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return domain.KubernetesHorizontalPodAutoscaler{}, fmt.Errorf("decode Kubernetes HorizontalPodAutoscaler: %w", domain.ErrUpstream)
	}
	if err := validateGovernanceIdentity(
		source.APIVersion, source.Kind, "autoscaling/v2", "HorizontalPodAutoscaler", source.Metadata, expectedNamespace,
	); err != nil {
		return domain.KubernetesHorizontalPodAutoscaler{}, err
	}

	targetAPIVersion, err := governanceScalar(source.Spec.ScaleTargetRef.APIVersion, true)
	if err != nil {
		return domain.KubernetesHorizontalPodAutoscaler{}, fmt.Errorf("invalid Kubernetes autoscaler target api version: %w", domain.ErrUpstream)
	}
	targetKind, err := governanceScalar(source.Spec.ScaleTargetRef.Kind, false)
	if err != nil || !validKubernetesMetadataString(source.Spec.ScaleTargetRef.Name) {
		return domain.KubernetesHorizontalPodAutoscaler{}, fmt.Errorf("invalid Kubernetes autoscaler target: %w", domain.ErrUpstream)
	}
	minReplicas := defaultHPAMinReplicas
	minDefaulted := source.Spec.MinReplicas == nil
	if source.Spec.MinReplicas != nil {
		minReplicas = *source.Spec.MinReplicas
	}
	if minReplicas < 0 || source.Spec.MaxReplicas <= 0 || source.Spec.MaxReplicas < minReplicas ||
		source.Status.CurrentReplicas < 0 || source.Status.DesiredReplicas < 0 {
		return domain.KubernetesHorizontalPodAutoscaler{}, fmt.Errorf("invalid Kubernetes autoscaler replica count: %w", domain.ErrUpstream)
	}
	if source.Status.LastScaleTime != nil && source.Status.LastScaleTime.IsZero() {
		return domain.KubernetesHorizontalPodAutoscaler{}, fmt.Errorf("invalid Kubernetes autoscaler scale time: %w", domain.ErrUpstream)
	}
	conditions, truncated, err := projectAvailabilityConditions(source.Status.Conditions, remainingConditions)
	if err != nil {
		return domain.KubernetesHorizontalPodAutoscaler{}, err
	}
	return domain.KubernetesHorizontalPodAutoscaler{
		Namespace: source.Metadata.Namespace, Name: source.Metadata.Name,
		TargetAPIVersion: targetAPIVersion, TargetKind: targetKind, TargetName: source.Spec.ScaleTargetRef.Name,
		MinReplicas: minReplicas, MinReplicasDefaulted: minDefaulted, MaxReplicas: source.Spec.MaxReplicas,
		CurrentReplicas: source.Status.CurrentReplicas, DesiredReplicas: source.Status.DesiredReplicas,
		MetricCount: len(source.Spec.Metrics), CurrentMetricCount: len(source.Status.CurrentMetrics),
		Observed:   generationObserved(source.Metadata.Generation, source.Status.ObservedGeneration),
		Conditions: conditions, ConditionCount: len(source.Status.Conditions), ConditionsTruncated: truncated,
		LastScaleTime: source.Status.LastScaleTime, CreatedAt: source.Metadata.CreationTimestamp,
	}, nil
}

func decodePodDisruptionBudget(
	raw json.RawMessage,
	expectedNamespace string,
	allNamespaces bool,
	remainingConditions *int,
) (domain.KubernetesPodDisruptionBudget, error) {
	var source struct {
		APIVersion string             `json:"apiVersion"`
		Kind       string             `json:"kind"`
		Metadata   governanceMetadata `json:"metadata"`
		Spec       struct {
			Selector                   *availabilitySelectorSource `json:"selector"`
			MinAvailable               json.RawMessage             `json:"minAvailable"`
			MaxUnavailable             json.RawMessage             `json:"maxUnavailable"`
			UnhealthyPodEvictionPolicy string                      `json:"unhealthyPodEvictionPolicy"`
		} `json:"spec"`
		Status struct {
			ObservedGeneration *int64                        `json:"observedGeneration"`
			CurrentHealthy     int32                         `json:"currentHealthy"`
			DesiredHealthy     int32                         `json:"desiredHealthy"`
			DisruptionsAllowed int32                         `json:"disruptionsAllowed"`
			ExpectedPods       int32                         `json:"expectedPods"`
			Conditions         []availabilityConditionSource `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return domain.KubernetesPodDisruptionBudget{}, fmt.Errorf("decode Kubernetes PodDisruptionBudget: %w", domain.ErrUpstream)
	}
	var identityErr error
	if allNamespaces {
		identityErr = validateGovernanceIdentityAnyNamespace(
			source.APIVersion, source.Kind, "policy/v1", "PodDisruptionBudget", source.Metadata,
		)
	} else {
		identityErr = validateGovernanceIdentity(
			source.APIVersion, source.Kind, "policy/v1", "PodDisruptionBudget", source.Metadata, expectedNamespace,
		)
	}
	if identityErr != nil {
		return domain.KubernetesPodDisruptionBudget{}, identityErr
	}
	if availabilityValuePresent(source.Spec.MinAvailable) && availabilityValuePresent(source.Spec.MaxUnavailable) {
		return domain.KubernetesPodDisruptionBudget{}, fmt.Errorf("Kubernetes disruption budget has mutually exclusive availability fields: %w", domain.ErrUpstream)
	}
	minAvailable, err := projectAvailabilityValue(source.Spec.MinAvailable)
	if err != nil {
		return domain.KubernetesPodDisruptionBudget{}, fmt.Errorf("invalid Kubernetes disruption budget minAvailable: %w", domain.ErrUpstream)
	}
	maxUnavailable, err := projectAvailabilityValue(source.Spec.MaxUnavailable)
	if err != nil {
		return domain.KubernetesPodDisruptionBudget{}, fmt.Errorf("invalid Kubernetes disruption budget maxUnavailable: %w", domain.ErrUpstream)
	}
	if source.Status.CurrentHealthy < 0 || source.Status.DesiredHealthy < 0 || source.Status.DisruptionsAllowed < 0 ||
		source.Status.ExpectedPods < 0 {
		return domain.KubernetesPodDisruptionBudget{}, fmt.Errorf("invalid Kubernetes disruption budget status count: %w", domain.ErrUpstream)
	}
	policy := source.Spec.UnhealthyPodEvictionPolicy
	policyDefaulted := policy == ""
	if policyDefaulted {
		policy = defaultPDBUnhealthyPolicy
	}
	policy, err = governanceScalar(policy, false)
	if err != nil {
		return domain.KubernetesPodDisruptionBudget{}, fmt.Errorf("invalid Kubernetes disruption budget unhealthy pod policy: %w", domain.ErrUpstream)
	}
	conditions, truncated, err := projectAvailabilityConditions(source.Status.Conditions, remainingConditions)
	if err != nil {
		return domain.KubernetesPodDisruptionBudget{}, err
	}
	selectorMode := domain.KubernetesSelectorNone
	selectorLabelCount := 0
	selectorExpressionCount := 0
	if source.Spec.Selector != nil {
		selectorLabelCount = len(source.Spec.Selector.MatchLabels)
		selectorExpressionCount = len(source.Spec.Selector.MatchExpressions)
		selectorMode = domain.KubernetesSelectorAll
		if selectorLabelCount > 0 || selectorExpressionCount > 0 {
			selectorMode = domain.KubernetesSelectorFiltered
		}
	}
	observed := generationObserved(source.Metadata.Generation, source.Status.ObservedGeneration)
	return domain.KubernetesPodDisruptionBudget{
		Namespace: source.Metadata.Namespace, Name: source.Metadata.Name,
		SelectorMode: selectorMode, SelectorLabelCount: selectorLabelCount, SelectorExpressionCount: selectorExpressionCount,
		MinAvailable: minAvailable, MaxUnavailable: maxUnavailable,
		CurrentHealthy: source.Status.CurrentHealthy, DesiredHealthy: source.Status.DesiredHealthy,
		DisruptionsAllowed: source.Status.DisruptionsAllowed, ExpectedPods: source.Status.ExpectedPods,
		Observed:                   observed,
		DisruptionStatus:           disruptionBudgetStatus(observed, source.Status.ExpectedPods, source.Status.DisruptionsAllowed),
		UnhealthyPodEvictionPolicy: policy, UnhealthyPodEvictionPolicyDefaulted: policyDefaulted,
		Conditions: conditions, ConditionCount: len(source.Status.Conditions), ConditionsTruncated: truncated,
		CreatedAt: source.Metadata.CreationTimestamp,
	}, nil
}

func disruptionBudgetStatus(
	observed bool,
	expectedPods, disruptionsAllowed int32,
) domain.KubernetesDisruptionBudgetStatus {
	if !observed {
		return domain.DisruptionBudgetUnobserved
	}
	if expectedPods == 0 {
		return domain.DisruptionBudgetInactive
	}
	if disruptionsAllowed == 0 {
		return domain.DisruptionBudgetBlocked
	}
	return domain.DisruptionBudgetAvailable
}

func projectAvailabilityConditions(
	source []availabilityConditionSource,
	remaining *int,
) ([]domain.KubernetesPolicyCondition, bool, error) {
	sorted := append([]availabilityConditionSource(nil), source...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Type != sorted[j].Type {
			return sorted[i].Type < sorted[j].Type
		}
		return sorted[i].Reason < sorted[j].Reason
	})
	limit := min(len(sorted), maxAvailabilityConditionsPerObject, max(0, *remaining))
	conditions := make([]domain.KubernetesPolicyCondition, 0, limit)
	for _, condition := range sorted[:limit] {
		conditionType, err := governanceScalar(condition.Type, false)
		if err != nil || !validConditionStatus(condition.Status) {
			return nil, false, fmt.Errorf("invalid Kubernetes availability condition: %w", domain.ErrUpstream)
		}
		reason, err := governanceScalar(condition.Reason, true)
		if err != nil {
			return nil, false, fmt.Errorf("invalid Kubernetes availability condition reason: %w", domain.ErrUpstream)
		}
		conditions = append(conditions, domain.KubernetesPolicyCondition{
			Type: conditionType, Status: condition.Status, Reason: reason,
		})
	}
	*remaining -= limit
	return conditions, len(sorted) > limit, nil
}

func projectAvailabilityValue(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if len(trimmed) > maxAvailabilityIntOrStringBytes+2 {
		return "", domain.ErrUpstream
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil || len(value) > maxAvailabilityIntOrStringBytes {
			return "", domain.ErrUpstream
		}
		return governanceScalar(value, false)
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return "", domain.ErrUpstream
	}
	value, err := strconv.ParseInt(number.String(), 10, 32)
	if err != nil || value < 0 {
		return "", domain.ErrUpstream
	}
	return strconv.FormatInt(value, 10), nil
}

func availabilityValuePresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func generationObserved(generation int64, observed *int64) bool {
	return generation > 0 && observed != nil && *observed == generation
}

func validConditionStatus(status string) bool {
	return status == "True" || status == "False" || status == "Unknown"
}
