package entity

import "time"

type Repository struct {
	ID                int64
	Provider          string
	Owner             string
	Name              string
	DefaultBranch     string
	GitHubAccountName string
	Status            string
	MattermostChannel string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (repo Repository) FullName() string {
	return repo.Owner + "/" + repo.Name
}
