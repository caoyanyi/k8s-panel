package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	schemaVersion        = 1
	maxStoredOperations  = 2000
	maxStoredAuditEvents = 5000
)

type state struct {
	SchemaVersion int                 `json:"schema_version"`
	Clusters      []domain.Cluster    `json:"clusters"`
	Repositories  []domain.Repository `json:"repositories"`
	Operations    []domain.Operation  `json:"operations"`
	AuditEvents   []domain.AuditEvent `json:"audit_events"`
}

type File struct {
	mu    sync.RWMutex
	path  string
	clock func() time.Time
	data  state
}

func Open(path string, clock func() time.Time) (*File, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store path must not be empty")
	}
	if clock == nil {
		clock = time.Now
	}
	store := &File{
		path:  path,
		clock: clock,
		data:  state{SchemaVersion: schemaVersion},
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	if err := store.reconcileHistoryRetention(); err != nil {
		return nil, err
	}
	if err := store.reconcileInterruptedOperations(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *File) reconcileHistoryRetention() error {
	next := cloneState(s.data)
	next.Operations = trimOperationHistory(next.Operations)
	next.AuditEvents = trimAuditHistory(next.AuditEvents)
	if len(next.Operations) == len(s.data.Operations) && len(next.AuditEvents) == len(s.data.AuditEvents) {
		return nil
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *File) load() error {
	contents, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read store: %w", err)
	}
	var loaded state
	if err := json.Unmarshal(contents, &loaded); err != nil {
		return fmt.Errorf("decode store: %w", err)
	}
	if loaded.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported store schema version %d", loaded.SchemaVersion)
	}
	s.data = loaded
	return nil
}

func (s *File) reconcileInterruptedOperations() error {
	next := cloneState(s.data)
	changed := false
	now := s.clock().UTC()
	for i := range next.Operations {
		if next.Operations[i].State != domain.OperationRunning && next.Operations[i].State != domain.OperationQueued {
			continue
		}
		next.Operations[i].State = domain.OperationUnknown
		next.Operations[i].ErrorCode = "process_restarted"
		next.Operations[i].ErrorMessage = "操作排队或执行期间服务发生重启，结果需要人工确认"
		next.Operations[i].UpdatedAt = now
		next.Operations[i].FinishedAt = now
		changed = true
	}
	if !changed {
		return nil
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *File) CreateCluster(ctx context.Context, cluster domain.Cluster) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.Clusters {
		if existing.ID == cluster.ID || strings.EqualFold(existing.Name, cluster.Name) {
			return domain.ErrConflict
		}
	}
	next := cloneState(s.data)
	next.Clusters = append(next.Clusters, cluster)
	return s.commit(next)
}

func (s *File) ListClusters(ctx context.Context) ([]domain.Cluster, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]domain.Cluster(nil), s.data.Clusters...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func (s *File) GetCluster(ctx context.Context, id string) (domain.Cluster, error) {
	if err := ctx.Err(); err != nil {
		return domain.Cluster{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, cluster := range s.data.Clusters {
		if cluster.ID == id {
			return cluster, nil
		}
	}
	return domain.Cluster{}, domain.ErrNotFound
}

func (s *File) UpdateCluster(ctx context.Context, cluster domain.Cluster) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.data)
	found := false
	for i := range next.Clusters {
		if next.Clusters[i].ID == cluster.ID {
			next.Clusters[i] = cluster
			found = true
			continue
		}
		if strings.EqualFold(next.Clusters[i].Name, cluster.Name) {
			return domain.ErrConflict
		}
	}
	if !found {
		return domain.ErrNotFound
	}
	return s.commit(next)
}

func (s *File) RotateClusterCredentials(
	ctx context.Context,
	expected domain.Cluster,
	updated domain.Cluster,
	audit domain.AuditEvent,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected.ID == "" || updated.ID != expected.ID {
		return errors.New("credential rotation cluster is invalid")
	}
	if audit.ID == "" || audit.ClusterID != updated.ID {
		return errors.New("credential rotation audit is invalid")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	clusterIndex := -1
	for index := range s.data.Clusters {
		if s.data.Clusters[index].ID != expected.ID {
			continue
		}
		clusterIndex = index
		if s.data.Clusters[index] != expected {
			return domain.ErrConflict
		}
		break
	}
	if clusterIndex < 0 {
		return domain.ErrNotFound
	}
	for _, existing := range s.data.AuditEvents {
		if existing.ID == audit.ID {
			return domain.ErrConflict
		}
	}

	next := cloneState(s.data)
	next.Clusters[clusterIndex] = updated
	next.AuditEvents = append(next.AuditEvents, audit)
	next.AuditEvents = trimAuditHistory(next.AuditEvents)
	return s.commit(next)
}

func (s *File) DeleteCluster(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.data)
	for i := range next.Clusters {
		if next.Clusters[i].ID != id {
			continue
		}
		next.Clusters = append(next.Clusters[:i], next.Clusters[i+1:]...)
		return s.commit(next)
	}
	return domain.ErrNotFound
}

func (s *File) CreateRepository(ctx context.Context, repository domain.Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.Repositories {
		if existing.ID == repository.ID || strings.EqualFold(existing.Name, repository.Name) {
			return domain.ErrConflict
		}
	}
	next := cloneState(s.data)
	next.Repositories = append(next.Repositories, repository)
	return s.commit(next)
}

func (s *File) ListRepositories(ctx context.Context) ([]domain.Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]domain.Repository(nil), s.data.Repositories...)
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func (s *File) GetRepository(ctx context.Context, id string) (domain.Repository, error) {
	if err := ctx.Err(); err != nil {
		return domain.Repository{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, repository := range s.data.Repositories {
		if repository.ID == id {
			return repository, nil
		}
	}
	return domain.Repository{}, domain.ErrNotFound
}

func (s *File) UpdateRepository(ctx context.Context, repository domain.Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.data)
	found := false
	for i := range next.Repositories {
		if next.Repositories[i].ID == repository.ID {
			next.Repositories[i] = repository
			found = true
			continue
		}
		if strings.EqualFold(next.Repositories[i].Name, repository.Name) {
			return domain.ErrConflict
		}
	}
	if !found {
		return domain.ErrNotFound
	}
	return s.commit(next)
}

func (s *File) DeleteRepository(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.data)
	for i := range next.Repositories {
		if next.Repositories[i].ID != id {
			continue
		}
		next.Repositories = append(next.Repositories[:i], next.Repositories[i+1:]...)
		return s.commit(next)
	}
	return domain.ErrNotFound
}

func (s *File) CreateOperation(ctx context.Context, operation domain.Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.Operations {
		if existing.ID == operation.ID {
			return domain.ErrConflict
		}
	}
	next := cloneState(s.data)
	next.Operations = append(next.Operations, operation)
	next.Operations = trimOperationHistory(next.Operations)
	return s.commit(next)
}

func (s *File) GetOperation(ctx context.Context, id string) (domain.Operation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Operation{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, operation := range s.data.Operations {
		if operation.ID == id {
			return operation, nil
		}
	}
	return domain.Operation{}, domain.ErrNotFound
}

func (s *File) UpdateOperation(ctx context.Context, operation domain.Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.data)
	for i := range next.Operations {
		if next.Operations[i].ID != operation.ID {
			continue
		}
		next.Operations[i] = operation
		return s.commit(next)
	}
	return domain.ErrNotFound
}

func (s *File) TransitionOperation(
	ctx context.Context,
	expected domain.OperationState,
	operation domain.Operation,
	audit *domain.AuditEvent,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if operation.ID == "" {
		return errors.New("operation ID is required")
	}
	if audit != nil && (audit.ID == "" || audit.OperationID != operation.ID) {
		return errors.New("operation transition audit is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	operationIndex := -1
	for index := range s.data.Operations {
		if s.data.Operations[index].ID == operation.ID {
			operationIndex = index
			break
		}
	}
	if operationIndex < 0 {
		return domain.ErrNotFound
	}
	if s.data.Operations[operationIndex].State != expected {
		return domain.ErrInvalidState
	}
	if audit != nil {
		for _, existing := range s.data.AuditEvents {
			if existing.ID == audit.ID {
				return domain.ErrConflict
			}
		}
	}

	next := cloneState(s.data)
	next.Operations[operationIndex] = operation
	if audit != nil {
		next.AuditEvents = append(next.AuditEvents, *audit)
		next.AuditEvents = trimAuditHistory(next.AuditEvents)
	}
	return s.commit(next)
}

func (s *File) ListOperations(ctx context.Context, limit int) ([]domain.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]domain.Operation(nil), s.data.Operations...)
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return limitSlice(items, limit), nil
}

func (s *File) CreateAuditEvent(ctx context.Context, event domain.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.AuditEvents {
		if existing.ID == event.ID {
			return domain.ErrConflict
		}
	}
	next := cloneState(s.data)
	next.AuditEvents = append(next.AuditEvents, event)
	next.AuditEvents = trimAuditHistory(next.AuditEvents)
	return s.commit(next)
}

func (s *File) ListAuditEvents(ctx context.Context, limit int) ([]domain.AuditEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]domain.AuditEvent(nil), s.data.AuditEvents...)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return limitSlice(items, limit), nil
}

func (s *File) commit(next state) error {
	if err := s.persist(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *File) persist(next state) error {
	contents, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".panel-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary store: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect temporary store: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary store: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary store: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary store: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace store: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("protect store: %w", err)
	}
	dirHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open store directory: %w", err)
	}
	defer dirHandle.Close()
	if err := dirHandle.Sync(); err != nil {
		return fmt.Errorf("sync store directory: %w", err)
	}
	return nil
}

func cloneState(source state) state {
	return state{
		SchemaVersion: source.SchemaVersion,
		Clusters:      append([]domain.Cluster(nil), source.Clusters...),
		Repositories:  append([]domain.Repository(nil), source.Repositories...),
		Operations:    append([]domain.Operation(nil), source.Operations...),
		AuditEvents:   append([]domain.AuditEvent(nil), source.AuditEvents...),
	}
}

func trimOperationHistory(operations []domain.Operation) []domain.Operation {
	overflow := len(operations) - maxStoredOperations
	if overflow <= 0 {
		return operations
	}
	trimmed := make([]domain.Operation, 0, maxStoredOperations)
	for _, operation := range operations {
		inFlight := operation.State == domain.OperationQueued || operation.State == domain.OperationRunning
		if overflow > 0 && !inFlight {
			overflow--
			continue
		}
		trimmed = append(trimmed, operation)
	}
	return trimmed
}

func trimAuditHistory(events []domain.AuditEvent) []domain.AuditEvent {
	if len(events) <= maxStoredAuditEvents {
		return events
	}
	trimmed := make([]domain.AuditEvent, maxStoredAuditEvents)
	copy(trimmed, events[len(events)-maxStoredAuditEvents:])
	return trimmed
}

func limitSlice[T any](items []T, limit int) []T {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if len(items) > limit {
		return items[:limit]
	}
	return items
}
