package entity

import "time"

type GitHubAccount struct {
	ID           int64
	Name         string
	CredentialID int64
	SecretRef    string
	Username     string
	Email        string
	Scopes       string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
