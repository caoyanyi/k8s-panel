package platform

import (
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

type ClusterView struct {
	ID                    string               `json:"id"`
	Name                  string               `json:"name"`
	Environment           domain.Environment   `json:"environment"`
	Server                string               `json:"server"`
	Status                domain.ClusterStatus `json:"status"`
	Version               string               `json:"version,omitempty"`
	LastErrorCode         string               `json:"last_error_code,omitempty"`
	CredentialsConfigured bool                 `json:"credentials_configured"`
	LastCheckedAt         time.Time            `json:"last_checked_at,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

type RepositoryView struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	URL                   string    `json:"url"`
	Enabled               bool      `json:"enabled"`
	Status                string    `json:"status"`
	LastErrorCode         string    `json:"last_error_code,omitempty"`
	CredentialsConfigured bool      `json:"credentials_configured"`
	LastCheckedAt         time.Time `json:"last_checked_at,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func clusterView(cluster domain.Cluster) ClusterView {
	return ClusterView{
		ID:                    cluster.ID,
		Name:                  cluster.Name,
		Environment:           cluster.Environment,
		Server:                cluster.Server,
		Status:                cluster.Status,
		Version:               cluster.Version,
		LastErrorCode:         cluster.LastErrorCode,
		CredentialsConfigured: cluster.BearerTokenCiphertext != "",
		LastCheckedAt:         cluster.LastCheckedAt,
		CreatedAt:             cluster.CreatedAt,
		UpdatedAt:             cluster.UpdatedAt,
	}
}

func repositoryView(repository domain.Repository) RepositoryView {
	return RepositoryView{
		ID:                    repository.ID,
		Name:                  repository.Name,
		URL:                   repository.URL,
		Enabled:               repository.Enabled,
		Status:                repository.Status,
		LastErrorCode:         repository.LastErrorCode,
		CredentialsConfigured: repository.UsernameCiphertext != "" || repository.PasswordCiphertext != "",
		LastCheckedAt:         repository.LastCheckedAt,
		CreatedAt:             repository.CreatedAt,
		UpdatedAt:             repository.UpdatedAt,
	}
}
