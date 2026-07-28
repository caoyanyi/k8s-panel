package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientReadsBoundedClusterEvents(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requested := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/payments/events" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("limit"); got != "250" {
			t.Errorf("limit = %q, want 250", got)
		}
		if got := r.URL.Query().Get("fieldSelector"); got != "type=Warning" {
			t.Errorf("fieldSelector = %q, want type=Warning", got)
		}
		mu.Lock()
		requested = append(requested, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, eventListResponse("", []any{
				testEvent("new-warning", "payments", "Warning", "BackOff", "Pod", "gateway-0", "kubelet", 4,
					"Back-off\n restarting container", "2026-07-28T08:05:00Z", map[string]any{
						"annotations": map[string]any{"private.example/token": "must-not-be-projected"},
					}),
			}))
			return
		}
		writeTestJSON(t, w, eventListResponse("page-two", []any{
			testEvent("old-warning", "payments", "Warning", "FailedScheduling", "Pod", "gateway-1", "default-scheduler", 2,
				"0/3 nodes are available", "2026-07-28T08:00:00Z", nil),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	events, err := client.Events(context.Background(), "payments", "Warning", 2)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(events) != 2 || events[0].Name != "new-warning" || events[1].Name != "old-warning" {
		t.Fatalf("Events() = %#v", events)
	}
	first := events[0]
	if first.Namespace != "payments" || first.Type != "Warning" || first.Reason != "BackOff" ||
		first.ObjectKind != "Pod" || first.ObjectName != "gateway-0" || first.Source != "kubelet" ||
		first.Count != 4 || first.Message != "Back-off restarting container" || first.MessageTruncated ||
		first.FirstSeen.IsZero() || first.LastSeen.IsZero() || first.CreatedAt.IsZero() {
		t.Fatalf("first event = %#v", first)
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(encoded), "must-not-be-projected") || strings.Contains(string(encoded), "annotations") {
		t.Fatalf("Events() projected unknown metadata: %s", encoded)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requested...)
	mu.Unlock()
	wantRequests := []string{
		"/api/v1/namespaces/payments/events?fieldSelector=type%3DWarning&limit=250",
		"/api/v1/namespaces/payments/events?continue=page-two&fieldSelector=type%3DWarning&limit=250",
	}
	if len(gotRequests) != len(wantRequests) {
		t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
	}
	for index := range wantRequests {
		if gotRequests[index] != wantRequests[index] {
			t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
		}
	}
}

func TestClientFiltersClusterEventsWithoutEmptySelector(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" {
			http.NotFound(w, r)
			return
		}
		if _, exists := r.URL.Query()["fieldSelector"]; exists {
			t.Errorf("unexpected fieldSelector in %s", r.URL.RequestURI())
		}
		writeTestJSON(t, w, eventListResponse("", []any{}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.Events(context.Background(), "", "", 200); err != nil {
		t.Fatalf("Events() error = %v", err)
	}
}

func TestClientValidatesEventFiltersBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	checks := []struct {
		name      string
		namespace string
		eventType string
		limit     int
	}{
		{name: "namespace", namespace: "bad/namespace", eventType: "Warning", limit: 200},
		{name: "type", namespace: "payments", eventType: "warning", limit: 200},
		{name: "empty limit", namespace: "payments", eventType: "Warning", limit: 0},
		{name: "large limit", namespace: "payments", eventType: "Warning", limit: domain.MaxClusterEventLimit + 1},
	}
	for _, check := range checks {
		if _, err := client.Events(context.Background(), check.namespace, check.eventType, check.limit); err == nil {
			t.Errorf("Events() accepted invalid %s", check.name)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid event filters made %d requests", got)
	}
}

func TestClientBoundsEventPagesAndSanitizesMessages(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeTestJSON(t, w, eventListResponse("more", []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.Events(context.Background(), "", "Warning", 200); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("Events() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != 8 {
			t.Fatalf("requests = %d, want 8", got)
		}
	})

	t.Run("message bytes", func(t *testing.T) {
		message := strings.Repeat("故障 ", 400) + "\x00 private-token"
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, eventListResponse("", []any{
				testEvent("warning", "payments", "Warning", "BackOff", "Pod", "gateway-0", "kubelet", 1,
					message, "2026-07-28T08:05:00Z", nil),
			}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		events, err := client.Events(context.Background(), "payments", "Warning", 200)
		if err != nil {
			t.Fatalf("Events() error = %v", err)
		}
		if len(events) != 1 || !events[0].MessageTruncated || len(events[0].Message) > 1024 ||
			!utf8.ValidString(events[0].Message) || strings.ContainsRune(events[0].Message, '\x00') {
			t.Fatalf("sanitized event = %#v", events)
		}
	})
}

func TestClientRejectsEventNamespaceEscapeWithoutLeakingBody(t *testing.T) {
	t.Parallel()

	const sensitiveMessage = "bearer-token-must-not-leak"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, eventListResponse("", []any{
			testEvent("warning", "other", "Warning", "BackOff", "Pod", "gateway-0", "kubelet", 1,
				sensitiveMessage, "2026-07-28T08:05:00Z", nil),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.Events(context.Background(), "payments", "Warning", 200)
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("Events() error = %v, want upstream error", err)
	}
	if strings.Contains(err.Error(), sensitiveMessage) {
		t.Fatalf("Events() error leaked event message: %v", err)
	}
}

func TestClientRejectsWorkloadEventNamespaceEscape(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/payments/events" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, map[string]any{"items": []any{
			testEvent("warning", "other", "Warning", "BackOff", "Pod", "gateway-0", "kubelet", 1,
				"cross-namespace event", "2026-07-28T08:05:00Z", nil),
		}})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.WorkloadEvents(context.Background(), domain.WorkloadReference{
		Kind: "pod", Namespace: "payments", Name: "gateway-0",
	}, 20)
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("WorkloadEvents() error = %v, want upstream error", err)
	}
}

func TestClientRejectsMalformedEventLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response func(*testing.T, http.ResponseWriter)
	}{
		{
			name: "invalid JSON",
			response: func(_ *testing.T, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"apiVersion":`))
			},
		},
		{
			name: "unsupported envelope",
			response: func(t *testing.T, w http.ResponseWriter) {
				writeTestJSON(t, w, map[string]any{"apiVersion": "events.k8s.io/v1", "kind": "EventList", "items": []any{}})
			},
		},
		{
			name: "item limit",
			response: func(t *testing.T, w http.ResponseWriter) {
				items := make([]any, maxEventListItems+1)
				for index := range items {
					items[index] = map[string]any{}
				}
				writeTestJSON(t, w, eventListResponse("", items))
			},
		},
		{
			name: "continuation token",
			response: func(t *testing.T, w http.ResponseWriter) {
				writeTestJSON(t, w, eventListResponse("unsafe\ncontinue", []any{}))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				test.response(t, w)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			if _, err := client.Events(context.Background(), "", "Warning", 200); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("Events() error = %v, want upstream error", err)
			}
		})
	}
}

func TestProjectEventRejectsUnsafeScalarsAndCounts(t *testing.T) {
	t.Parallel()

	valid := func() eventResource {
		resource := eventResource{
			Metadata: objectMetadata{
				Name: "warning", Namespace: "payments", CreationTimestamp: time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC),
			},
			Type: "Warning", Reason: "BackOff", Message: "container restarting", Count: 1,
			ReportingComponent: "kubelet",
			LastTimestamp:      time.Date(2026, 7, 28, 8, 1, 0, 0, time.UTC),
		}
		resource.InvolvedObject.Kind = "Pod"
		resource.InvolvedObject.Namespace = "payments"
		resource.InvolvedObject.Name = "gateway-0"
		return resource
	}
	tests := []struct {
		name   string
		mutate func(*eventResource)
	}{
		{name: "name", mutate: func(resource *eventResource) { resource.Metadata.Name = "" }},
		{name: "namespace", mutate: func(resource *eventResource) { resource.Metadata.Namespace = "bad\nnamespace" }},
		{name: "namespace syntax", mutate: func(resource *eventResource) { resource.Metadata.Namespace = "bad/namespace" }},
		{name: "type", mutate: func(resource *eventResource) { resource.Type = strings.Repeat("x", maxEventScalarBytes+1) }},
		{name: "type whitespace", mutate: func(resource *eventResource) { resource.Type = " Warning " }},
		{name: "reason", mutate: func(resource *eventResource) { resource.Reason = "bad\x00reason" }},
		{name: "source", mutate: func(resource *eventResource) { resource.ReportingComponent = "bad\nsource" }},
		{name: "object kind", mutate: func(resource *eventResource) { resource.InvolvedObject.Kind = "bad\nkind" }},
		{name: "object name", mutate: func(resource *eventResource) { resource.InvolvedObject.Name = "bad\nname" }},
		{name: "object namespace", mutate: func(resource *eventResource) { resource.InvolvedObject.Namespace = "bad/namespace" }},
		{name: "object namespace escape", mutate: func(resource *eventResource) { resource.InvolvedObject.Namespace = "other" }},
		{name: "count", mutate: func(resource *eventResource) { resource.Series.Count = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := valid()
			test.mutate(&resource)
			if _, err := projectEvent(resource); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("projectEvent() error = %v, want upstream error", err)
			}
		})
	}

	if _, err := decodeEvents([]json.RawMessage{json.RawMessage(`{"metadata":`)}, 1); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("decodeEvents() error = %v, want upstream error", err)
	}
}

func eventListResponse(continueToken string, items []any) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "EventList",
		"metadata":   map[string]any{"continue": continueToken},
		"items":      items,
	}
}

func testEvent(
	name, namespace, eventType, reason, objectKind, objectName, source string,
	count int,
	message, lastSeen string,
	metadataExtra map[string]any,
) map[string]any {
	metadata := map[string]any{
		"name": name, "namespace": namespace, "creationTimestamp": "2026-07-28T07:55:00Z",
	}
	for key, value := range metadataExtra {
		metadata[key] = value
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Event",
		"metadata":   metadata,
		"type":       eventType,
		"reason":     reason,
		"message":    message,
		"count":      count,
		"source":     map[string]any{"component": source},
		"involvedObject": map[string]any{
			"kind": objectKind, "namespace": namespace, "name": objectName,
		},
		"firstTimestamp": "2026-07-28T07:56:00Z",
		"lastTimestamp":  lastSeen,
		"unknown":        map[string]any{"secret": "must-not-be-projected"},
	}
}
