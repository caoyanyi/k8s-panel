package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestFileStorePersistsClustersAndProtectsFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "panel.json")
	clock := func() time.Time { return time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC) }
	store, err := Open(path, clock)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	cluster := domain.Cluster{
		ID:                    "clu_one",
		Name:                  "production-east",
		Environment:           domain.EnvironmentProduction,
		Server:                "https://api.example.com:6443",
		Status:                domain.ClusterPending,
		BearerTokenCiphertext: "v1.ciphertext",
		CreatedAt:             clock(),
		UpdatedAt:             clock(),
	}
	if err := store.CreateCluster(context.Background(), cluster); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}

	reopened, err := Open(path, clock)
	if err != nil {
		t.Fatalf("Open() second error = %v", err)
	}
	got, err := reopened.GetCluster(context.Background(), cluster.ID)
	if err != nil {
		t.Fatalf("GetCluster() error = %v", err)
	}
	if got != cluster {
		t.Errorf("GetCluster() = %#v, want %#v", got, cluster)
	}
}

func TestFileStoreRejectsDuplicateClusterName(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "panel.json"), time.Now)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ctx := context.Background()
	first := domain.Cluster{ID: "clu_one", Name: "shared", Server: "https://one.example.com"}
	second := domain.Cluster{ID: "clu_two", Name: "shared", Server: "https://two.example.com"}
	if err := store.CreateCluster(ctx, first); err != nil {
		t.Fatalf("CreateCluster(first) error = %v", err)
	}
	if err := store.CreateCluster(ctx, second); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("CreateCluster(second) error = %v, want ErrConflict", err)
	}
}

func TestFileStoreMarksUnrecoverableOperationsUnknown(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "panel.json")
	started := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	store, err := Open(path, func() time.Time { return started })
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	operations := []domain.Operation{
		{ID: "op_running", Kind: domain.OperationHelmInstall, State: domain.OperationRunning, CreatedAt: started, UpdatedAt: started},
		{ID: "op_queued", Kind: domain.OperationWorkloadScale, State: domain.OperationQueued, CreatedAt: started, UpdatedAt: started},
		{ID: "op_finished", Kind: domain.OperationHelmInstall, State: domain.OperationSucceeded, CreatedAt: started, UpdatedAt: started},
	}
	for _, operation := range operations {
		if err := store.CreateOperation(context.Background(), operation); err != nil {
			t.Fatalf("CreateOperation(%s) error = %v", operation.ID, err)
		}
	}

	restarted := started.Add(time.Minute)
	reopened, err := Open(path, func() time.Time { return restarted })
	if err != nil {
		t.Fatalf("Open() second error = %v", err)
	}
	for _, id := range []string{"op_running", "op_queued"} {
		got, getErr := reopened.GetOperation(context.Background(), id)
		if getErr != nil {
			t.Fatalf("GetOperation(%s) error = %v", id, getErr)
		}
		if got.State != domain.OperationUnknown || got.ErrorCode != "process_restarted" || !got.UpdatedAt.Equal(restarted) {
			t.Errorf("reconciled operation = %#v", got)
		}
	}
	finished, err := reopened.GetOperation(context.Background(), "op_finished")
	if err != nil || finished.State != domain.OperationSucceeded {
		t.Errorf("finished operation = %#v, %v", finished, err)
	}
}

func TestFileStoreTransitionsOperationAndAuditAtomically(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "panel.json")
	started := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	fileStore, err := Open(path, func() time.Time { return started })
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	operation := domain.Operation{
		ID: "op_queued", Kind: domain.OperationWorkloadScale, State: domain.OperationQueued,
		Target: "gateway", CreatedAt: started, UpdatedAt: started,
	}
	if err := fileStore.CreateOperation(context.Background(), operation); err != nil {
		t.Fatalf("CreateOperation() error = %v", err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fileStore.TransitionOperation(
		canceledContext, domain.OperationQueued, operation, nil,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("TransitionOperation(canceled context) error = %v", err)
	}
	if err := fileStore.TransitionOperation(
		context.Background(), domain.OperationQueued, domain.Operation{}, nil,
	); err == nil {
		t.Fatal("TransitionOperation() accepted an empty operation ID")
	}
	if err := fileStore.TransitionOperation(
		context.Background(), domain.OperationQueued, operation,
		&domain.AuditEvent{ID: "audit_invalid", OperationID: "op_other"},
	); err == nil {
		t.Fatal("TransitionOperation() accepted a mismatched audit operation ID")
	}
	missing := operation
	missing.ID = "op_missing"
	if err := fileStore.TransitionOperation(
		context.Background(), domain.OperationQueued, missing, nil,
	); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("TransitionOperation(missing) error = %v, want ErrNotFound", err)
	}
	canceled := operation
	canceled.State = domain.OperationCanceled
	canceled.FinishedAt = finished
	canceled.UpdatedAt = finished
	audit := domain.AuditEvent{
		ID: "audit_cancel", RequestID: "req_cancel", OperationID: operation.ID,
		Actor: "admin", Action: "operation.cancel", Result: "succeeded", Target: operation.Target, CreatedAt: finished,
	}
	if err := fileStore.TransitionOperation(
		context.Background(), domain.OperationQueued, canceled, &audit,
	); err != nil {
		t.Fatalf("TransitionOperation() error = %v", err)
	}
	if err := fileStore.TransitionOperation(
		context.Background(), domain.OperationQueued, canceled, &audit,
	); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("duplicate TransitionOperation() error = %v, want ErrInvalidState", err)
	}

	reopened, err := Open(path, func() time.Time { return finished })
	if err != nil {
		t.Fatalf("Open() second error = %v", err)
	}
	got, err := reopened.GetOperation(context.Background(), operation.ID)
	if err != nil || got.State != domain.OperationCanceled || !got.FinishedAt.Equal(finished) {
		t.Fatalf("canceled operation = %#v, error = %v", got, err)
	}
	audits, err := reopened.ListAuditEvents(context.Background(), 100)
	if err != nil || len(audits) != 1 || audits[0].ID != audit.ID || audits[0].OperationID != operation.ID {
		t.Fatalf("cancel audits = %#v, error = %v", audits, err)
	}
	other := operation
	other.ID = "op_other"
	if err := reopened.CreateOperation(context.Background(), other); err != nil {
		t.Fatalf("CreateOperation(other) error = %v", err)
	}
	other.State = domain.OperationCanceled
	duplicateAudit := audit
	duplicateAudit.OperationID = other.ID
	if err := reopened.TransitionOperation(
		context.Background(), domain.OperationQueued, other, &duplicateAudit,
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("TransitionOperation(duplicate audit) error = %v, want ErrConflict", err)
	}
}

func TestHistoryRetentionKeepsActiveOperationsAndNewestRecords(t *testing.T) {
	t.Parallel()

	operations := make([]domain.Operation, 0, maxStoredOperations+2)
	operations = append(operations, domain.Operation{ID: "op_active", State: domain.OperationRunning})
	for index := 0; index < maxStoredOperations; index++ {
		operations = append(operations, domain.Operation{ID: fmt.Sprintf("op_%04d", index), State: domain.OperationSucceeded})
	}
	operations = append(operations, domain.Operation{ID: "op_new", State: domain.OperationQueued})
	trimmedOperations := trimOperationHistory(operations)
	if len(trimmedOperations) != maxStoredOperations {
		t.Fatalf("operation history size = %d", len(trimmedOperations))
	}
	if !containsOperation(trimmedOperations, "op_active") || !containsOperation(trimmedOperations, "op_new") {
		t.Fatalf("active operations were pruned: %#v", trimmedOperations)
	}
	if containsOperation(trimmedOperations, "op_0000") || containsOperation(trimmedOperations, "op_0001") {
		t.Fatal("oldest completed operations were retained")
	}

	audits := make([]domain.AuditEvent, maxStoredAuditEvents+1)
	for index := range audits {
		audits[index].ID = fmt.Sprintf("audit_%04d", index)
	}
	trimmedAudits := trimAuditHistory(audits)
	if len(trimmedAudits) != maxStoredAuditEvents || trimmedAudits[0].ID != "audit_0001" {
		t.Fatalf("audit history was not trimmed from the oldest edge: first=%q size=%d", trimmedAudits[0].ID, len(trimmedAudits))
	}
}

func containsOperation(operations []domain.Operation, id string) bool {
	for _, operation := range operations {
		if operation.ID == id {
			return true
		}
	}
	return false
}
