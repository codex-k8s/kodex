package session

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Зарегистрированные capability-роли PostgreSQL.
const (
	CapabilityIssuer                       = "internal_rpc_authority_issuer"
	CapabilityVerifier                     = "internal_rpc_authority_verifier"
	CapabilityDatabaseCredentialReconciler = "internal_rpc_authority_database_credential_reconciler"
	CapabilityPublisher                    = "internal_rpc_authority_publisher"
	CapabilityReadbackAttestor             = "internal_rpc_authority_readback_attestor"
	CapabilityRestoreController            = "internal_rpc_authority_restore_controller"
)

// Configure проверяет LOGIN principal и активирует точную NOLOGIN-роль.
func Configure(
	ctx context.Context,
	connection *pgx.Conn,
	expectedSessionUser string,
	capability string,
) error {
	if connection == nil || expectedSessionUser == "" {
		return errors.New("PostgreSQL session configuration is invalid")
	}
	queries, err := loadQueries()
	if err != nil {
		return err
	}
	sessionUser, currentUser, err := identity(ctx, connection, queries.sessionIdentity)
	if err != nil {
		return errors.New("read PostgreSQL session identity")
	}
	if sessionUser != expectedSessionUser || currentUser != expectedSessionUser {
		return errors.New("PostgreSQL initial session identity mismatch")
	}
	assume, err := assumeQuery(queries, capability)
	if err != nil {
		return err
	}
	if _, err := connection.Exec(ctx, assume); err != nil {
		return errors.New("activate PostgreSQL capability role")
	}
	sessionUser, currentUser, err = identity(ctx, connection, queries.sessionIdentity)
	if err != nil {
		return errors.New("read effective PostgreSQL identity")
	}
	if sessionUser != expectedSessionUser || currentUser != capability {
		return errors.New("PostgreSQL effective identity mismatch")
	}
	return nil
}

// Ensure подтверждает capability-роль соединения перед повторной выдачей из
// пула и восстанавливает её только из ожидаемого LOGIN principal.
func Ensure(
	ctx context.Context,
	connection *pgx.Conn,
	expectedSessionUser string,
	capability string,
) error {
	if connection == nil || expectedSessionUser == "" {
		return errors.New("PostgreSQL session configuration is invalid")
	}
	queries, err := loadQueries()
	if err != nil {
		return err
	}
	sessionUser, currentUser, err := identity(ctx, connection, queries.sessionIdentity)
	if err != nil {
		return errors.New("read PostgreSQL session identity")
	}
	if sessionUser != expectedSessionUser {
		return errors.New("PostgreSQL session user mismatch")
	}
	if currentUser == capability {
		return nil
	}
	if currentUser != expectedSessionUser {
		return errors.New("PostgreSQL effective identity mismatch")
	}
	assume, err := assumeQuery(queries, capability)
	if err != nil {
		return err
	}
	if _, err := connection.Exec(ctx, assume); err != nil {
		return errors.New("activate PostgreSQL capability role")
	}
	sessionUser, currentUser, err = identity(ctx, connection, queries.sessionIdentity)
	if err != nil {
		return errors.New("read effective PostgreSQL identity")
	}
	if sessionUser != expectedSessionUser || currentUser != capability {
		return errors.New("PostgreSQL effective identity mismatch")
	}
	return nil
}

func identity(
	ctx context.Context,
	connection *pgx.Conn,
	query string,
) (string, string, error) {
	var sessionUser string
	var currentUser string
	err := connection.QueryRow(ctx, query).Scan(&sessionUser, &currentUser)
	return sessionUser, currentUser, err
}

func assumeQuery(queries querySet, capability string) (string, error) {
	switch capability {
	case CapabilityIssuer:
		return queries.issuerAssume, nil
	case CapabilityVerifier:
		return queries.verifierAssume, nil
	case CapabilityDatabaseCredentialReconciler:
		return queries.databaseCredentialReconcilerAssume, nil
	case CapabilityPublisher:
		return queries.publisherAssume, nil
	case CapabilityReadbackAttestor:
		return queries.readbackAttestorAssume, nil
	case CapabilityRestoreController:
		return queries.restoreControllerAssume, nil
	default:
		return "", errors.New("PostgreSQL capability role is not registered")
	}
}
