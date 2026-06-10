package entity

import "time"

type OpenAIAccount struct {
	ID           int64
	Name         string
	CredentialID int64
	SecretRef    string
	Status       string
	ModelPolicy  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
