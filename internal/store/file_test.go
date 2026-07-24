package store

import (
	"context"
	"errors"
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

func TestFileStoreMarksInterruptedOperationsUnknown(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "panel.json")
	started := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	store, err := Open(path, func() time.Time { return started })
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	op := domain.Operation{
		ID:        "op_one",
		Kind:      domain.OperationHelmInstall,
		State:     domain.OperationRunning,
		CreatedAt: started,
		UpdatedAt: started,
	}
	if err := store.CreateOperation(context.Background(), op); err != nil {
		t.Fatalf("CreateOperation() error = %v", err)
	}

	restarted := started.Add(time.Minute)
	reopened, err := Open(path, func() time.Time { return restarted })
	if err != nil {
		t.Fatalf("Open() second error = %v", err)
	}
	got, err := reopened.GetOperation(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("GetOperation() error = %v", err)
	}
	if got.State != domain.OperationUnknown {
		t.Errorf("state = %q, want %q", got.State, domain.OperationUnknown)
	}
	if got.ErrorCode != "process_restarted" {
		t.Errorf("error code = %q, want process_restarted", got.ErrorCode)
	}
	if !got.UpdatedAt.Equal(restarted) {
		t.Errorf("updated at = %v, want %v", got.UpdatedAt, restarted)
	}
}
