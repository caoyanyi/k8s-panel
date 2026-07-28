package kubernetes

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientListsBatchWorkloads(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/batch/v1/namespaces/payments/jobs":
			writeTestJSON(t, w, map[string]any{"items": []any{
				map[string]any{
					"metadata": map[string]any{
						"name": "daily-settlement", "namespace": "payments",
						"creationTimestamp": "2026-07-28T01:00:00Z",
					},
					"spec": map[string]any{
						"completions": 4,
						"parallelism": 2,
						"template": map[string]any{"spec": map[string]any{"containers": []any{
							map[string]any{"name": "settlement", "image": "registry.example.com/settlement:1.8.0"},
						}}},
					},
					"status": map[string]any{"active": 1, "succeeded": 2, "failed": 1},
				},
			}})
		case "/apis/batch/v1/namespaces/payments/cronjobs":
			writeTestJSON(t, w, map[string]any{"items": []any{
				map[string]any{
					"metadata": map[string]any{
						"name": "nightly-report", "namespace": "payments",
						"creationTimestamp": "2026-07-27T01:00:00Z",
					},
					"spec": map[string]any{
						"schedule": "0 2 * * *",
						"jobTemplate": map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{
							map[string]any{"name": "report", "image": "registry.example.com/report:2.3.0"},
						}}}}},
					},
					"status": map[string]any{
						"active":           []any{map[string]any{"name": "nightly-report-100"}},
						"lastScheduleTime": "2026-07-28T02:00:00Z",
					},
				},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	jobs, err := client.Workloads(context.Background(), "payments", "job")
	if err != nil {
		t.Fatalf("Workloads(job) error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].Kind != "Job" || jobs[0].Ready != 2 || jobs[0].Desired != 4 || jobs[0].Status != "Running" {
		t.Fatalf("jobs = %#v", jobs)
	}
	if len(jobs[0].Images) != 1 || jobs[0].Images[0] != "registry.example.com/settlement:1.8.0" {
		t.Errorf("job images = %#v", jobs[0].Images)
	}

	cronJobs, err := client.Workloads(context.Background(), "payments", "cronjob")
	if err != nil {
		t.Fatalf("Workloads(cronjob) error = %v", err)
	}
	if len(cronJobs) != 1 || cronJobs[0].Kind != "CronJob" || cronJobs[0].Ready != 1 || cronJobs[0].Desired != 1 || cronJobs[0].Status != "Running" {
		t.Fatalf("cronJobs = %#v", cronJobs)
	}
	if len(cronJobs[0].Images) != 1 || cronJobs[0].Images[0] != "registry.example.com/report:2.3.0" {
		t.Errorf("CronJob images = %#v", cronJobs[0].Images)
	}
}

func TestClientReadsBatchWorkloadDetailsAndRedactsCronJobTemplate(t *testing.T) {
	t.Parallel()

	var eventSelector string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/batch/v1/namespaces/payments/jobs/daily-settlement":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "batch/v1", "kind": "Job",
				"metadata": map[string]any{
					"name": "daily-settlement", "namespace": "payments", "uid": "uid-job", "resourceVersion": "51",
					"creationTimestamp": "2026-07-28T01:00:00Z", "labels": map[string]string{"app": "settlement"},
				},
				"spec": map[string]any{
					"completions": 4,
					"template": map[string]any{"spec": map[string]any{"containers": []any{
						map[string]any{"name": "settlement", "image": "registry.example.com/settlement:1.8.0"},
					}}},
				},
				"status": map[string]any{
					"succeeded": 4,
					"conditions": []any{map[string]any{
						"type": "Complete", "status": "True", "reason": "CompletionsReached",
						"lastTransitionTime": "2026-07-28T01:04:00Z",
					}},
				},
			})
		case "/apis/batch/v1/namespaces/payments/cronjobs/nightly-report":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "batch/v1", "kind": "CronJob",
				"metadata": map[string]any{
					"name": "nightly-report", "namespace": "payments", "uid": "uid-cronjob", "resourceVersion": "72",
					"creationTimestamp": "2026-07-27T01:00:00Z", "labels": map[string]string{"app": "report"},
				},
				"spec": map[string]any{
					"schedule": "0 2 * * *",
					"jobTemplate": map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{
						map[string]any{
							"name": "report", "image": "registry.example.com/report:2.3.0",
							"command": []any{"/bin/sh", "-c", "report --token=command-secret"},
							"env":     []any{map[string]any{"name": "REPORT_TOKEN", "value": "literal-secret"}},
						},
					}}}}},
				},
				"status": map[string]any{
					"lastScheduleTime": "2026-07-28T02:00:00Z",
				},
			})
		case "/api/v1/namespaces/payments/events":
			eventSelector = r.URL.Query().Get("fieldSelector")
			writeTestJSON(t, w, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	job, err := client.WorkloadDetail(context.Background(), domain.WorkloadReference{
		Kind: "job", Namespace: "payments", Name: "daily-settlement",
	})
	if err != nil {
		t.Fatalf("WorkloadDetail(job) error = %v", err)
	}
	if job.Kind != "Job" || job.Status != "Succeeded" || job.Ready != 4 || job.Desired != 4 || len(job.Containers) != 1 {
		t.Errorf("job detail = %#v", job)
	}
	if len(job.Conditions) != 1 || job.Conditions[0].Type != "Complete" {
		t.Errorf("job conditions = %#v", job.Conditions)
	}

	cronJob, err := client.WorkloadDetail(context.Background(), domain.WorkloadReference{
		Kind: "cronjob", Namespace: "payments", Name: "nightly-report",
	})
	if err != nil {
		t.Fatalf("WorkloadDetail(cronjob) error = %v", err)
	}
	if cronJob.Kind != "CronJob" || cronJob.Status != "Scheduled" || len(cronJob.Containers) != 1 || cronJob.Containers[0].Name != "report" {
		t.Errorf("CronJob detail = %#v", cronJob)
	}
	for _, forbidden := range []string{"literal-secret", "command-secret", "status:"} {
		if strings.Contains(cronJob.YAML, forbidden) {
			t.Errorf("sanitized CronJob YAML contains %q:\n%s", forbidden, cronJob.YAML)
		}
	}
	if !strings.Contains(cronJob.YAML, "<redacted>") || !strings.Contains(cronJob.YAML, "schedule: 0 2 * * *") {
		t.Errorf("sanitized CronJob YAML is missing safe fields or redaction:\n%s", cronJob.YAML)
	}

	if _, err := client.WorkloadEvents(context.Background(), domain.WorkloadReference{
		Kind: "cronjob", Namespace: "payments", Name: "nightly-report",
	}, 10); err != nil {
		t.Fatalf("WorkloadEvents(cronjob) error = %v", err)
	}
	if !strings.Contains(eventSelector, "involvedObject.kind=CronJob") {
		t.Errorf("CronJob event selector = %q", eventSelector)
	}
}

func TestClientSharesWorkloadListBudgetAcrossKinds(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requested := make([]string, 0, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/payments/deployments":
			writeTestJSON(t, w, map[string]any{"items": repeatedEmptyObjects(3000)})
		case "/apis/apps/v1/namespaces/payments/statefulsets":
			writeTestJSON(t, w, map[string]any{"items": repeatedEmptyObjects(2001)})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	if _, err := client.Workloads(context.Background(), "payments", ""); !errors.Is(err, domain.ErrUpstream) || !strings.Contains(err.Error(), "safe item limit") {
		t.Fatalf("Workloads() error = %v, want shared item limit", err)
	}
	mu.Lock()
	joined := strings.Join(requested, "\n")
	mu.Unlock()
	if strings.Contains(joined, "/daemonsets") || strings.Contains(joined, "/jobs") {
		t.Errorf("workload reads continued after the shared budget was exhausted:\n%s", joined)
	}
}

func TestJobStatusUsesControllerConditionsBeforeCounters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		suspended  bool
		active     int32
		succeeded  int32
		failed     int32
		desired    int32
		conditions []workloadCondition
		want       string
	}{
		{name: "complete condition", active: 1, desired: 4, conditions: []workloadCondition{{Type: "Complete", Status: "True"}}, want: "Succeeded"},
		{name: "failed condition", active: 1, desired: 4, conditions: []workloadCondition{{Type: "Failed", Status: "True"}}, want: "Failed"},
		{name: "suspended condition", active: 1, desired: 4, conditions: []workloadCondition{{Type: "Suspended", Status: "True"}}, want: "Suspended"},
		{name: "suspended spec", suspended: true, desired: 1, want: "Suspended"},
		{name: "running", active: 2, desired: 4, want: "Running"},
		{name: "completed counters", succeeded: 4, desired: 4, want: "Succeeded"},
		{name: "retrying", failed: 1, desired: 4, want: "Retrying"},
		{name: "pending", desired: 1, conditions: []workloadCondition{{Type: "Failed", Status: "False"}}, want: "Pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jobStatus(tt.suspended, tt.active, tt.succeeded, tt.failed, tt.desired, tt.conditions); got != tt.want {
				t.Errorf("jobStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func newBatchTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{
		Server: server.URL, CACert: string(certificate), BearerToken: "test-token",
	}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func repeatedEmptyObjects(count int) []any {
	items := make([]any, count)
	for index := range items {
		items[index] = map[string]any{}
	}
	return items
}
