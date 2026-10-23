package session

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Зарегистрированные capability-роли PostgreSQL.
const (
	// CapabilityIssuer is the registered issuer capability-role name used to select a fixed SET ROLE statement; the constant grants no PostgreSQL permission.
	// CapabilityIssuer — зарегистрированное имя capability-role issuer для выбора фиксированного SET ROLE; константа не выдаёт прав PostgreSQL.
	CapabilityIssuer = "internal_rpc_authority_issuer"
	// CapabilityVerifier names the verifier's effective database role, distinct from the immutable LOGIN session_user checked before and after role activation.
	// CapabilityVerifier задаёт эффективную роль БД verifier отдельно от неизменяемого LOGIN session_user, проверяемого до и после принятия роли.
	CapabilityVerifier = "internal_rpc_authority_verifier"
	// CapabilityPublisher selects the publisher role-switch profile, not a caller-provided SQL identifier or newly created login.
	// CapabilityPublisher выбирает профиль переключения роли publisher, а не SQL-идентификатор от вызывающего или новый LOGIN.
	CapabilityPublisher = "internal_rpc_authority_publisher"
	// CapabilityReadbackAttestor selects the fixed readback-attestor capability statement; membership and credential lifecycle are enforced outside the string constant.
	// CapabilityReadbackAttestor выбирает фиксированный capability statement readback-attestor; членство и жизненный цикл credential обеспечиваются вне строковой константы.
	CapabilityReadbackAttestor = "internal_rpc_authority_readback_attestor"
	// CapabilityRestoreController selects the restore-controller effective-role profile without expanding runtime privileges or assigning a database owner.
	// CapabilityRestoreController выбирает профиль эффективной роли restore-controller без расширения runtime-прав и назначения владельца БД.
	CapabilityRestoreController = "internal_rpc_authority_restore_controller"
)

// Configure requires initial session_user and current_user to equal the expected LOGIN, selects a registered capability statement, executes it and verifies unchanged LOGIN plus the exact effective role. Failure does not itself reset an already switched role; the connection owner handles rejection.
// Configure требует исходные session_user и current_user равными ожидаемому LOGIN, выбирает зарегистрированный capability statement, исполняет его и проверяет прежний LOGIN и точную эффективную роль. Ошибка сама не сбрасывает уже принятую роль; соединение отклоняет его владелец.
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
	return activateCapability(ctx, connection, queries.sessionIdentity, expectedSessionUser, capability, assume)
}

// activateCapability executes the caller-selected registered SET ROLE and
// rereads the unchanged LOGIN and exact effective role using the same context.
// It neither selects arbitrary roles nor resets a switched role after failure;
// connection ownership and rejection remain with the pool configuration caller.
// activateCapability исполняет выбранный вызывающим зарегистрированный SET ROLE
// и перечитывает прежний LOGIN и точную эффективную роль под тем же контекстом.
// Произвольные роли не выбираются, принятая роль после ошибки не сбрасывается;
// владение соединением и его отклонение остаются у настройки пула.
func activateCapability(ctx context.Context, connection *pgx.Conn, identityQuery, expectedSessionUser, capability, assume string) error {
	if _, err := connection.Exec(ctx, assume); err != nil {
		return errors.New("activate PostgreSQL capability role")
	}
	sessionUser, currentUser, err := identity(ctx, connection, identityQuery)
	if err != nil {
		return errors.New("read effective PostgreSQL identity")
	}
	if sessionUser != expectedSessionUser || currentUser != capability {
		return errors.New("PostgreSQL effective identity mismatch")
	}
	return nil
}

// Ensure requires the expected immutable LOGIN, returns immediately for an already matching effective capability, otherwise only switches from the original LOGIN through the closed query selector. It rejects another effective role and verifies identity after switching; the fast path does not reselect the registry.
// Ensure требует ожидаемый неизменяемый LOGIN, сразу возвращается при совпавшей эффективной capability, иначе переключает только исходный LOGIN через закрытый selector запросов. Другая эффективная роль отклоняется, после переключения проверяется идентичность; быстрый путь повторно не обращается к реестру.
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
	return activateCapability(ctx, connection, queries.sessionIdentity, expectedSessionUser, capability, assume)
}

// identity reads session_user and current_user with the supplied fixed identity query and returns the scan error unchanged; it neither changes roles nor interprets caller authority.
// identity читает session_user и current_user переданным фиксированным запросом идентичности и возвращает ошибку Scan без изменения; роли не меняет и полномочия вызывающего не интерпретирует.
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

// assumeQuery maps only the five registered capability names to embedded SET ROLE statements and rejects every other string without SQL interpolation or execution.
// assumeQuery преобразует только пять зарегистрированных имён capability во встроенные SET ROLE и отклоняет остальные строки без подстановки в SQL и исполнения.
func assumeQuery(queries querySet, capability string) (string, error) {
	switch capability {
	case CapabilityIssuer:
		return queries.issuerAssume, nil
	case CapabilityVerifier:
		return queries.verifierAssume, nil
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
