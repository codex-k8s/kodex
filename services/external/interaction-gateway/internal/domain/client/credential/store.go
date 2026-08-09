// Package credential отделяет узкий Vault-owned lifecycle bot credential от
// provider adapter и не предоставляет generic secret CRUD.
package credential

import "context"

type Materialized struct {
	BindingID     string
	SecretRef     string
	Version       uint64
	ContentSHA256 string
}

type Store interface {
	MaterializeBotToken(context.Context, string, string) (Materialized, error)
	RecoverBotToken(context.Context, string) (Materialized, error)
	ReadBotToken(context.Context, string, uint64, string) (string, error)
	RevokeBotToken(context.Context, string, uint64) (bool, error)
	CheckBotTokenRevoked(context.Context, string, uint64) error
	Check(context.Context) error
}
