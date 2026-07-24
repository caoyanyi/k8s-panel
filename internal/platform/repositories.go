package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func (s *Service) CreateRepository(
	ctx context.Context,
	actor string,
	requestID string,
	input domain.RepositoryInput,
) (RepositoryView, error) {
	if err := domain.ValidateRepositoryInput(input); err != nil {
		return RepositoryView{}, err
	}
	if err := s.targetValidator.Validate(ctx, input.URL); err != nil {
		return RepositoryView{}, domain.Invalid("url", "target is blocked or cannot be resolved")
	}
	id, err := s.newID("repo")
	if err != nil {
		return RepositoryView{}, fmt.Errorf("create repository ID: %w", err)
	}
	var usernameCiphertext, passwordCiphertext string
	if input.Username != "" {
		usernameCiphertext, err = s.cipher.SealString(input.Username, repositoryAAD(id, "username"))
		if err != nil {
			return RepositoryView{}, fmt.Errorf("encrypt repository username: %w", err)
		}
	}
	if input.Password != "" {
		passwordCiphertext, err = s.cipher.SealString(input.Password, repositoryAAD(id, "password"))
		if err != nil {
			return RepositoryView{}, fmt.Errorf("encrypt repository password: %w", err)
		}
	}
	now := s.now()
	repository := domain.Repository{
		ID:                 id,
		Name:               input.Name,
		URL:                strings.TrimRight(input.URL, "/"),
		Enabled:            true,
		UsernameCiphertext: usernameCiphertext,
		PasswordCiphertext: passwordCiphertext,
		Status:             "pending",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.store.CreateRepository(ctx, repository); err != nil {
		return RepositoryView{}, err
	}
	if err := s.audit(ctx, actor, requestID, "chart_repository.create", "succeeded", "", "", repository.Name, "repository metadata created", ""); err != nil {
		return RepositoryView{}, err
	}
	view, _ := s.testRepository(ctx, actor, requestID, id)
	return view, nil
}

func (s *Service) ListRepositories(ctx context.Context) ([]RepositoryView, error) {
	repositories, err := s.store.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]RepositoryView, 0, len(repositories))
	for _, repository := range repositories {
		views = append(views, repositoryView(repository))
	}
	return views, nil
}

func (s *Service) TestRepository(ctx context.Context, actor, requestID, id string) (RepositoryView, error) {
	return s.testRepository(ctx, actor, requestID, id)
}

func (s *Service) testRepository(ctx context.Context, actor, requestID, id string) (RepositoryView, error) {
	repository, err := s.store.GetRepository(ctx, id)
	if err != nil {
		return RepositoryView{}, err
	}
	if !repository.Enabled {
		return RepositoryView{}, domain.ErrInvalidState
	}
	connection, err := s.repositoryConnection(repository)
	if err != nil {
		return RepositoryView{}, err
	}
	checkErr := s.repositoryChecker.Check(ctx, connection)
	repository.LastCheckedAt = s.now()
	repository.UpdatedAt = repository.LastCheckedAt
	result := "succeeded"
	if checkErr == nil {
		repository.Status = "connected"
		repository.LastErrorCode = ""
	} else {
		repository.Status = "unreachable"
		repository.LastErrorCode = "repository_unavailable"
		result = "failed"
	}
	if err := s.store.UpdateRepository(ctx, repository); err != nil {
		return RepositoryView{}, err
	}
	if err := s.audit(ctx, actor, requestID, "chart_repository.connection_test", result, "", "", repository.Name, repository.LastErrorCode, ""); err != nil {
		return RepositoryView{}, err
	}
	return repositoryView(repository), checkErr
}

func (s *Service) SetRepositoryEnabled(ctx context.Context, actor, requestID, id string, enabled bool) (RepositoryView, error) {
	repository, err := s.store.GetRepository(ctx, id)
	if err != nil {
		return RepositoryView{}, err
	}
	repository.Enabled = enabled
	if enabled {
		repository.Status = "pending"
	} else {
		repository.Status = "disabled"
	}
	repository.UpdatedAt = s.now()
	if err := s.store.UpdateRepository(ctx, repository); err != nil {
		return RepositoryView{}, err
	}
	if err := s.audit(ctx, actor, requestID, "chart_repository.update", "succeeded", "", "", repository.Name, fmt.Sprintf("enabled=%t", enabled), ""); err != nil {
		return RepositoryView{}, err
	}
	return repositoryView(repository), nil
}

func (s *Service) DeleteRepository(ctx context.Context, actor, requestID, id string) error {
	repository, err := s.store.GetRepository(ctx, id)
	if err != nil {
		return err
	}
	if err := s.audit(ctx, actor, requestID, "chart_repository.delete", "succeeded", "", "", repository.Name, "repository metadata deleted", ""); err != nil {
		return err
	}
	return s.store.DeleteRepository(ctx, id)
}

func (s *Service) repositoryConnection(repository domain.Repository) (RepositoryConnection, error) {
	connection := RepositoryConnection{URL: repository.URL}
	var err error
	if repository.UsernameCiphertext != "" {
		connection.Username, err = s.cipher.OpenString(repository.UsernameCiphertext, repositoryAAD(repository.ID, "username"))
		if err != nil {
			return RepositoryConnection{}, fmt.Errorf("decrypt repository username: %w", err)
		}
	}
	if repository.PasswordCiphertext != "" {
		connection.Password, err = s.cipher.OpenString(repository.PasswordCiphertext, repositoryAAD(repository.ID, "password"))
		if err != nil {
			return RepositoryConnection{}, fmt.Errorf("decrypt repository password: %w", err)
		}
	}
	return connection, nil
}

func repositoryAAD(id, field string) string {
	return "repository:" + id + ":" + field
}
