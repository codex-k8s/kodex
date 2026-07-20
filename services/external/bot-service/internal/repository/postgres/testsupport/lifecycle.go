// Package testsupport предоставляет единый безопасный lifecycle одноразовой PostgreSQL для тестов.
package testsupport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const vectorExtensionLockName = "mattercodex.test.vector-extension.v1"

const (
	disposableMarkerPrefix        = "mattercodex-disposable-v2"
	disposableDatabasePrefix      = "mc_test_"
	disposableMarkerLifetime      = 6 * time.Hour
	disposableMarkerClockSkew     = 5 * time.Minute
	bootstrapProofVersion         = 1
	bootstrapProofPurpose         = "mattercodex-postgres-integration-tests"
	bootstrapProofState           = "unconsumed"
	bootstrapProofLifetime        = 10 * time.Minute
	bootstrapProofClockSkew       = 30 * time.Second
	bootstrapProofTable           = "mattercodex_test_bootstrap_proofs"
	bootstrapProofGuardFunction   = "mattercodex_test_bootstrap_proofs_guard"
	bootstrapProofGuardTrigger    = "mattercodex_test_bootstrap_proofs_immutable"
	testDatabaseDSNEnvironment    = "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"
	testDatabaseMarkerEnvironment = "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER"
	testBootstrapProofEnvironment = "MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF"
	bootstrapTargetStateReserved  = "reserved"
	bootstrapTargetStateCreated   = "created"
	bootstrapTargetStateMarked    = "marked"
	bootstrapTargetStateDropped   = "dropped"
	bootstrapCleanupAttempts      = 4
	bootstrapCleanupTimeout       = 20 * time.Second
	bootstrapCleanupAttempt       = 5 * time.Second
	bootstrapCleanupRetryDelay    = 100 * time.Millisecond
)

var resourceSequence atomic.Uint64
var disposableMarkers sync.Map

type DisposableDatabase struct {
	DSN      string
	Marker   string
	Database string
}

type disposableMarker struct {
	issuedAt            time.Time
	expiresAt           time.Time
	token               string
	endpointFingerprint string
	serverFingerprint   string
	runID               string
}

type bootstrapProof struct {
	Version             int    `json:"version"`
	Nonce               string `json:"nonce"`
	NonceSHA256         string `json:"nonce_sha256"`
	IssuedAt            string `json:"issued_at"`
	ExpiresAt           string `json:"expires_at"`
	EndpointFingerprint string `json:"endpoint_fingerprint"`
	ServerFingerprint   string `json:"server_fingerprint"`
	MaintenanceDatabase string `json:"maintenance_database"`
	Purpose             string `json:"purpose"`
	RunID               string `json:"run_id"`
	State               string `json:"state"`
}

type parsedBootstrapProof struct {
	bootstrapProof
	issuedAt  time.Time
	expiresAt time.Time
	nonceHash []byte
}

type postgresServerIdentity struct {
	currentDatabase     string
	serverAddress       string
	serverPort          int
	dataDirectory       string
	systemIdentifier    string
	currentUserName     string
	currentUserOID      int64
	endpointFingerprint string
	serverFingerprint   string
}

type bootstrapTargetLedger struct {
	database        string
	markerSHA256    []byte
	ownerOID        int64
	databaseOID     int64
	state           string
	maintenanceDB   string
	endpointBinding string
	serverBinding   string
}

type bootstrapLifecycleHookPoint string

const (
	bootstrapHookBeforeCreateExec      bootstrapLifecycleHookPoint = "before_create_exec"
	bootstrapHookAfterCreateExec       bootstrapLifecycleHookPoint = "after_create_exec"
	bootstrapHookAfterCreateIdentified bootstrapLifecycleHookPoint = "after_create_identified"
	bootstrapHookBeforeComment         bootstrapLifecycleHookPoint = "before_comment"
	bootstrapHookAfterCommentExec      bootstrapLifecycleHookPoint = "after_comment_exec"
	bootstrapHookBeforeDeriveDSN       bootstrapLifecycleHookPoint = "before_derive_dsn"
	bootstrapHookBeforeFinalValidate   bootstrapLifecycleHookPoint = "before_final_validate"
	bootstrapHookAfterFinalValidate    bootstrapLifecycleHookPoint = "after_final_validate"
	bootstrapHookBeforeCleanupAttempt  bootstrapLifecycleHookPoint = "before_cleanup_attempt"
	bootstrapHookBeforeDrop            bootstrapLifecycleHookPoint = "before_drop"
	bootstrapHookAfterDropExec         bootstrapLifecycleHookPoint = "after_drop_exec"
)

type bootstrapLifecycleHookInput struct {
	bootstrapDSN string
	target       DisposableDatabase
	databaseOID  int64
}

type bootstrapLifecycleOptions struct {
	hook                       func(context.Context, bootstrapLifecycleHookPoint, bootstrapLifecycleHookInput) error
	createDatabaseOIDForTest   int64
	createDatabaseErrorForTest error
	cleanupTimeout             time.Duration
	attemptTimeout             time.Duration
	retryDelay                 time.Duration
	attempts                   int
}

type bootstrapLifecycleOptionsContextKey struct{}

type bootstrapCleanupError struct {
	message   string
	retryable bool
}

type bootstrapCreateCollisionError struct {
	sqlState string
}

func (err bootstrapCreateCollisionError) Error() string {
	return "создание одноразовой PostgreSQL database отклонено однозначной коллизией; резервирование использовано, удаление объекта-сироты запрещено"
}

func (err bootstrapCleanupError) Error() string {
	return err.message
}

// ExternalCleanupRequiredError передаёт удаление exact database владельцу
// внешнего ephemeral endpoint, не раскрывая DSN, proof или marker.
type ExternalCleanupRequiredError struct {
	Database string
}

func (err ExternalCleanupRequiredError) Error() string {
	return "PostgreSQL test target сохранён; destructive cleanup должен выполнить владелец ephemeral endpoint/controller"
}

func RequiredDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(testDatabaseDSNEnvironment))
	if dsn == "" {
		if os.Getenv("MATTERCODEX_POSTGRES_TEST_REQUIRED") == "1" {
			t.Fatal("одноразовый PostgreSQL target обязателен в required-режиме")
		}
		t.Skip("одноразовый PostgreSQL target не задан")
	}
	marker, err := markerForDSN(dsn)
	if err != nil {
		t.Fatal("одноразовый PostgreSQL target не имеет допустимой per-run identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ValidateDisposableDatabase(ctx, dsn, marker); err != nil {
		t.Fatal("одноразовый PostgreSQL target не прошёл fail-closed admission")
	}
	return dsn
}

func ValidateDisposableDatabase(ctx context.Context, dsn string, markerValue string) error {
	config, marker, err := validateDisposableIdentityOffline(dsn, markerValue, time.Now().UTC())
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("одноразовый PostgreSQL target недоступен")
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("одноразовый PostgreSQL target недоступен")
	}
	defer connection.Release()
	return validateDisposableDatabaseConnection(ctx, connection, config, markerValue, marker)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func validateDisposableDatabaseConnection(ctx context.Context, querier rowQuerier, config *pgxpool.Config, markerValue string, marker disposableMarker) error {
	identity, err := readPostgresServerIdentity(ctx, querier, config)
	if err != nil {
		return err
	}
	var databaseComment string
	if err := querier.QueryRow(ctx, `
select coalesce(shobj_description(oid, 'pg_database'), '')
from pg_database
where datname = current_database()
`).Scan(&databaseComment); err != nil {
		return fmt.Errorf("read-only проверка одноразового PostgreSQL target не выполнена")
	}
	if identity.currentDatabase != config.ConnConfig.Database || identity.currentDatabase != disposableDatabaseName(markerValue) || databaseComment != markerValue ||
		identity.endpointFingerprint != marker.endpointFingerprint || identity.serverFingerprint != marker.serverFingerprint {
		return fmt.Errorf("одноразовый PostgreSQL target имеет несовпадающую identity")
	}
	if time.Now().UTC().After(marker.expiresAt) {
		return fmt.Errorf("одноразовый PostgreSQL target просрочен")
	}
	return nil
}

func readPostgresServerIdentity(ctx context.Context, querier rowQuerier, config *pgxpool.Config) (postgresServerIdentity, error) {
	var identity postgresServerIdentity
	if err := querier.QueryRow(ctx, `
select current_database(), coalesce(host(inet_server_addr()), ''), coalesce(inet_server_port(), 0),
	current_setting('data_directory'), system_identifier::text, current_user,
	(select oid::bigint from pg_roles where rolname = current_user)
from pg_control_system()
`).Scan(
		&identity.currentDatabase, &identity.serverAddress, &identity.serverPort,
		&identity.dataDirectory, &identity.systemIdentifier, &identity.currentUserName, &identity.currentUserOID,
	); err != nil {
		return postgresServerIdentity{}, fmt.Errorf("read-only проверка PostgreSQL server identity не выполнена")
	}
	if identity.currentDatabase != strings.TrimSpace(config.ConnConfig.Database) {
		return postgresServerIdentity{}, fmt.Errorf("PostgreSQL maintenance database имеет несовпадающую identity")
	}
	if localEndpoint(config.ConnConfig.Host) {
		if identity.serverAddress != "" {
			address := net.ParseIP(strings.TrimSpace(identity.serverAddress))
			if address == nil || !address.IsLoopback() {
				return postgresServerIdentity{}, fmt.Errorf("локальный PostgreSQL endpoint разрешился во внешний endpoint")
			}
		}
	} else if identity.serverPort != int(config.ConnConfig.Port) {
		return postgresServerIdentity{}, fmt.Errorf("PostgreSQL endpoint подключён к несовпадающему server port")
	}
	identity.endpointFingerprint = sha256Text(strings.Join([]string{
		strings.TrimSpace(config.ConnConfig.Host), strconv.Itoa(int(config.ConnConfig.Port)),
		strings.TrimSpace(identity.serverAddress), strconv.Itoa(identity.serverPort),
	}, "\x00"))
	identity.serverFingerprint = sha256Text(strings.Join([]string{
		strings.TrimSpace(identity.systemIdentifier), strings.TrimSpace(identity.dataDirectory),
	}, "\x00"))
	return identity, nil
}

func sha256Text(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func definiteCreateDatabaseNotApplied(err error) (string, bool) {
	var postgresErr *pgconn.PgError
	if !errors.As(err, &postgresErr) {
		return "", false
	}
	return postgresErr.Code, postgresErr.Code == "42P04" || postgresErr.Code == "23505"
}

func BootstrapDisposableDatabase(ctx context.Context, bootstrapDSN string, proofValue string) (DisposableDatabase, error) {
	config, err := validateBootstrapEndpointOffline(bootstrapDSN)
	if err != nil {
		return DisposableDatabase{}, err
	}
	now := time.Now().UTC()
	proof, err := parseBootstrapProof(proofValue, now)
	if err != nil {
		return DisposableDatabase{}, err
	}
	markerValue, err := newDisposableMarker(
		now, proof.EndpointFingerprint, proof.ServerFingerprint, proof.RunID,
	)
	if err != nil {
		return DisposableDatabase{}, err
	}
	target := DisposableDatabase{Marker: markerValue, Database: disposableDatabaseName(markerValue)}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return DisposableDatabase{}, fmt.Errorf("bootstrap PostgreSQL endpoint недоступен")
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return DisposableDatabase{}, fmt.Errorf("bootstrap PostgreSQL endpoint недоступен")
	}
	defer connection.Release()
	identity, err := claimBootstrapProof(ctx, connection, config, proof, target)
	if err != nil {
		return DisposableDatabase{}, err
	}
	identifier := pgx.Identifier{target.Database}.Sanitize()
	owner := pgx.Identifier{identity.currentUserName}.Sanitize()
	options := bootstrapLifecycleOptionsFromContext(ctx)
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookBeforeCreateExec, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: options.createDatabaseOIDForTest,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "создание одноразовой PostgreSQL database не начато")
	}
	createStatement := "create database " + identifier + " with template template0 owner " + owner
	if options.createDatabaseOIDForTest != 0 {
		if options.createDatabaseOIDForTest < 1<<14 || options.createDatabaseOIDForTest > 1<<32-1 {
			return target, fmt.Errorf("тестовый OID PostgreSQL database вне допустимого диапазона")
		}
		createStatement += " oid " + strconv.FormatInt(options.createDatabaseOIDForTest, 10)
	}
	createErr := options.createDatabaseErrorForTest
	if createErr == nil {
		_, createErr = connection.Exec(ctx, createStatement)
	}
	if createErr != nil {
		if sqlState, definite := definiteCreateDatabaseNotApplied(createErr); definite {
			return target, bootstrapCreateCollisionError{sqlState: sqlState}
		}
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "создание одноразовой PostgreSQL database не выполнено")
	}
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookAfterCreateExec, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "результат создания одноразовой PostgreSQL database неоднозначен")
	}
	databaseOID, err := recordBootstrapTargetIdentity(context.WithoutCancel(ctx), bootstrapDSN, target, identity)
	if err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "identity созданной одноразовой PostgreSQL database не зафиксирована")
	}
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookAfterCreateIdentified, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: databaseOID,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "проверка после создания одноразовой PostgreSQL database не выполнена")
	}
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookBeforeComment, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: databaseOID,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "маркировка одноразовой PostgreSQL database не выполнена")
	}
	comment := strings.ReplaceAll(markerValue, "'", "''")
	if _, err := connection.Exec(ctx, "comment on database "+identifier+" is '"+comment+"'"); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "маркировка одноразовой PostgreSQL database не выполнена")
	}
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookAfterCommentExec, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: databaseOID,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "результат маркировки одноразовой PostgreSQL database неоднозначен")
	}
	if err := markBootstrapTargetComment(context.WithoutCancel(ctx), bootstrapDSN, target, databaseOID); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "identity marker одноразовой PostgreSQL database не зафиксирован")
	}
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookBeforeDeriveDSN, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: databaseOID,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "получение DSN одноразовой PostgreSQL database не выполнено")
	}
	targetDSN, err := deriveDSN(bootstrapDSN, target.Database, "public")
	if err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "получение DSN одноразовой PostgreSQL database не выполнено")
	}
	target.DSN = targetDSN
	disposableMarkers.Store(target.Database, markerValue)
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookBeforeFinalValidate, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: databaseOID,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "финальная проверка одноразовой PostgreSQL database не выполнена")
	}
	if err := ValidateDisposableDatabase(ctx, targetDSN, markerValue); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "финальная проверка одноразовой PostgreSQL database не выполнена")
	}
	if err := runBootstrapLifecycleHook(ctx, bootstrapHookAfterFinalValidate, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: databaseOID,
	}); err != nil {
		return failBootstrapWithCleanup(ctx, bootstrapDSN, target, "результат финальной проверки одноразовой PostgreSQL database неоднозначен")
	}
	return target, nil
}

func DestroyDisposableDatabase(ctx context.Context, bootstrapDSN string, target DisposableDatabase) error {
	if target.Database == "" || target.Database != disposableDatabaseName(target.Marker) {
		return fmt.Errorf("identity одноразовой PostgreSQL database не подтверждена")
	}
	if target.DSN == "" {
		var err error
		target.DSN, err = deriveDSN(bootstrapDSN, target.Database, "public")
		if err != nil {
			return fmt.Errorf("удаление PostgreSQL target отклонено точной identity")
		}
	}
	if err := boundedBootstrapTargetCleanup(ctx, bootstrapDSN, target, true, false); err != nil {
		return err
	}
	disposableMarkers.Delete(target.Database)
	return nil
}

func failBootstrapWithCleanup(
	ctx context.Context,
	bootstrapDSN string,
	target DisposableDatabase,
	failure string,
) (DisposableDatabase, error) {
	cleanupErr := boundedBootstrapTargetCleanup(ctx, bootstrapDSN, target, false, true)
	if cleanupErr != nil {
		return target, fmt.Errorf("%s; ограниченная компенсирующая очистка не подтверждена: %w", failure, cleanupErr)
	}
	disposableMarkers.Delete(target.Database)
	return target, fmt.Errorf("%s; отсутствие target подтверждено компенсирующей очисткой", failure)
}

func recordBootstrapTargetIdentity(
	ctx context.Context,
	bootstrapDSN string,
	target DisposableDatabase,
	claimedIdentity postgresServerIdentity,
) (int64, error) {
	options := bootstrapLifecycleOptionsFromContext(ctx)
	attemptCtx, cancel := context.WithTimeout(ctx, options.attemptTimeout)
	defer cancel()
	_, marker, pool, connection, identity, err := openBootstrapMaintenance(attemptCtx, bootstrapDSN, target)
	if err != nil {
		return 0, err
	}
	defer pool.Close()
	defer connection.Release()
	if identity.endpointFingerprint != claimedIdentity.endpointFingerprint ||
		identity.serverFingerprint != claimedIdentity.serverFingerprint ||
		identity.currentDatabase != claimedIdentity.currentDatabase ||
		identity.currentUserOID != claimedIdentity.currentUserOID {
		return 0, fmt.Errorf("созданный PostgreSQL target относится к несовпадающему server identity")
	}
	ledger, err := loadBootstrapTargetLedger(attemptCtx, connection, marker, target, identity)
	if err != nil {
		return 0, err
	}
	snapshot, err := readBootstrapTargetSnapshot(attemptCtx, connection, target.Database)
	if err != nil || !snapshot.exists {
		return 0, fmt.Errorf("созданный PostgreSQL target не найден для фиксации identity")
	}
	if err := validateBootstrapTargetSnapshot(snapshot, ledger, target.Marker, false); err != nil {
		return 0, err
	}
	if ledger.databaseOID == 0 {
		if err := setBootstrapTargetCreated(attemptCtx, connection, marker.runID, target, snapshot.databaseOID); err != nil {
			return 0, err
		}
		ledger.databaseOID = snapshot.databaseOID
		ledger.state = bootstrapTargetStateCreated
	}
	if snapshot.databaseOID != ledger.databaseOID {
		return 0, fmt.Errorf("созданный PostgreSQL target имеет несовпадающий OID")
	}
	confirmed, err := readBootstrapTargetSnapshot(attemptCtx, connection, target.Database)
	if err != nil || !confirmed.exists || confirmed.databaseOID != snapshot.databaseOID || confirmed.ownerOID != ledger.ownerOID {
		return 0, fmt.Errorf("созданный PostgreSQL target изменился при фиксации identity")
	}
	return snapshot.databaseOID, nil
}

func markBootstrapTargetComment(
	ctx context.Context,
	bootstrapDSN string,
	target DisposableDatabase,
	databaseOID int64,
) error {
	options := bootstrapLifecycleOptionsFromContext(ctx)
	attemptCtx, cancel := context.WithTimeout(ctx, options.attemptTimeout)
	defer cancel()
	_, marker, pool, connection, identity, err := openBootstrapMaintenance(attemptCtx, bootstrapDSN, target)
	if err != nil {
		return err
	}
	defer pool.Close()
	defer connection.Release()
	ledger, err := loadBootstrapTargetLedger(attemptCtx, connection, marker, target, identity)
	if err != nil {
		return err
	}
	snapshot, err := readBootstrapTargetSnapshot(attemptCtx, connection, target.Database)
	if err != nil || !snapshot.exists || snapshot.databaseOID != databaseOID || ledger.databaseOID != databaseOID {
		return fmt.Errorf("PostgreSQL target изменился до фиксации marker")
	}
	if err := validateBootstrapTargetSnapshot(snapshot, ledger, target.Marker, true); err != nil {
		return err
	}
	return setBootstrapTargetMarked(attemptCtx, connection, marker.runID, target, databaseOID)
}

func boundedBootstrapTargetCleanup(
	ctx context.Context,
	bootstrapDSN string,
	target DisposableDatabase,
	requireMarker bool,
	detachCaller bool,
) error {
	options := bootstrapLifecycleOptionsFromContext(ctx)
	parent := ctx
	if detachCaller {
		parent = context.WithoutCancel(ctx)
	}
	cleanupCtx, cancel := context.WithTimeout(parent, options.cleanupTimeout)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= options.attempts; attempt++ {
		attemptCtx, attemptCancel := context.WithTimeout(cleanupCtx, options.attemptTimeout)
		if hookErr := runBootstrapLifecycleHook(attemptCtx, bootstrapHookBeforeCleanupAttempt, bootstrapLifecycleHookInput{
			bootstrapDSN: bootstrapDSN, target: target,
		}); hookErr != nil {
			lastErr = bootstrapCleanupError{message: "временный отказ подключения при компенсирующей очистке", retryable: true}
			attemptCancel()
		} else {
			lastErr = cleanupBootstrapTargetOnce(attemptCtx, bootstrapDSN, target, requireMarker)
			attemptCancel()
			if lastErr == nil {
				return nil
			}
			var cleanupErr bootstrapCleanupError
			if !errors.As(lastErr, &cleanupErr) || !cleanupErr.retryable {
				return lastErr
			}
		}
		if attempt == options.attempts {
			break
		}
		timer := time.NewTimer(options.retryDelay)
		select {
		case <-cleanupCtx.Done():
			timer.Stop()
			return fmt.Errorf("истёк общий срок компенсирующей очистки PostgreSQL target")
		case <-timer.C:
		}
	}
	if cleanupCtx.Err() != nil {
		return fmt.Errorf("истёк общий срок компенсирующей очистки PostgreSQL target")
	}
	if lastErr == nil {
		return fmt.Errorf("компенсирующая очистка PostgreSQL target не выполнена")
	}
	return fmt.Errorf("компенсирующая очистка PostgreSQL target исчерпала повторы: %w", lastErr)
}

func cleanupBootstrapTargetOnce(
	ctx context.Context,
	bootstrapDSN string,
	target DisposableDatabase,
	requireMarker bool,
) error {
	config, marker, pool, connection, identity, err := openBootstrapMaintenance(ctx, bootstrapDSN, target)
	if err != nil {
		var cleanupErr bootstrapCleanupError
		if errors.As(err, &cleanupErr) {
			return cleanupErr
		}
		return bootstrapCleanupError{message: "bootstrap PostgreSQL endpoint недоступен при cleanup", retryable: true}
	}
	defer pool.Close()
	defer connection.Release()
	if _, err := connection.Exec(ctx, `select pg_advisory_lock(hashtextextended($1, 0))`, marker.runID); err != nil {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target не получил exact claim lock", retryable: true}
	}
	if err := validateBootstrapProofRegistry(ctx, connection); err != nil {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target не подтвердил immutable proof registry", retryable: false}
	}
	ledger, err := loadBootstrapTargetLedger(ctx, connection, marker, target, identity)
	if err != nil {
		return bootstrapCleanupError{message: err.Error(), retryable: false}
	}
	snapshot, err := readBootstrapTargetSnapshot(ctx, connection, target.Database)
	if err != nil {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target не прочитал exact database identity", retryable: true}
	}
	if !snapshot.exists {
		if !requireMarker && ledger.state != bootstrapTargetStateMarked && ledger.state != bootstrapTargetStateDropped {
			return bootstrapCleanupError{
				message:   "PostgreSQL target до exact applied marker сохранён для ручной сверки; consumed proof и ledger не изменены",
				retryable: false,
			}
		}
		if err := setBootstrapTargetDropped(ctx, connection, marker.runID, target, ledger.databaseOID); err != nil {
			return bootstrapCleanupError{message: "cleanup PostgreSQL target не зафиксировал подтверждённое отсутствие", retryable: true}
		}
		return nil
	}
	if ledger.state == bootstrapTargetStateDropped {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target обнаружил replacement после подтверждённого удаления", retryable: false}
	}
	if ledger.databaseOID == 0 || ledger.state != bootstrapTargetStateMarked {
		return bootstrapCleanupError{
			message:   "PostgreSQL target до exact applied marker сохранён как коллизия или объект-сирота; требуется ручная очистка владельцем endpoint",
			retryable: false,
		}
	}
	if err := validateBootstrapTargetSnapshot(snapshot, ledger, target.Marker, true); err != nil {
		return bootstrapCleanupError{message: err.Error(), retryable: false}
	}
	if err := requireRegisteredGeneratedPostgresAuthority(identity); err != nil {
		return ExternalCleanupRequiredError{Database: target.Database}
	}
	if requireMarker {
		if err := ValidateDisposableDatabase(ctx, target.DSN, target.Marker); err != nil {
			return bootstrapCleanupError{message: "удаление PostgreSQL target отклонено финальным admission", retryable: false}
		}
	}
	if hookErr := runBootstrapLifecycleHook(ctx, bootstrapHookBeforeDrop, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: snapshot.databaseOID,
	}); hookErr != nil {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target прерван до DROP", retryable: true}
	}
	finalIdentity, err := readPostgresServerIdentity(ctx, connection, config)
	if err != nil || finalIdentity.endpointFingerprint != identity.endpointFingerprint ||
		finalIdentity.serverFingerprint != identity.serverFingerprint || finalIdentity.currentDatabase != identity.currentDatabase {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target потерял exact server identity", retryable: false}
	}
	finalLedger, err := loadBootstrapTargetLedger(ctx, connection, marker, target, finalIdentity)
	if err != nil || finalLedger.databaseOID != ledger.databaseOID || finalLedger.ownerOID != ledger.ownerOID || finalLedger.state == bootstrapTargetStateDropped {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target потерял exact creation ledger", retryable: false}
	}
	finalSnapshot, err := readBootstrapTargetSnapshot(ctx, connection, target.Database)
	if err != nil {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target не выполнил финальную сверку database", retryable: true}
	}
	if !finalSnapshot.exists || finalSnapshot.databaseOID != snapshot.databaseOID || finalSnapshot.ownerOID != snapshot.ownerOID || finalSnapshot.comment != snapshot.comment {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target обнаружил replacement перед DROP", retryable: false}
	}
	if _, err := connection.Exec(ctx, "drop database "+pgx.Identifier{target.Database}.Sanitize()+" with (force)"); err != nil {
		return bootstrapCleanupError{message: "результат DROP DATABASE не подтверждён", retryable: true}
	}
	if hookErr := runBootstrapLifecycleHook(ctx, bootstrapHookAfterDropExec, bootstrapLifecycleHookInput{
		bootstrapDSN: bootstrapDSN, target: target, databaseOID: snapshot.databaseOID,
	}); hookErr != nil {
		return bootstrapCleanupError{message: "ответ DROP DATABASE потерян", retryable: true}
	}
	afterDrop, err := readBootstrapTargetSnapshot(ctx, connection, target.Database)
	if err != nil {
		return bootstrapCleanupError{message: "отсутствие PostgreSQL target после DROP не подтверждено", retryable: true}
	}
	if afterDrop.exists {
		if afterDrop.databaseOID != snapshot.databaseOID {
			return bootstrapCleanupError{message: "после DROP обнаружен replacement PostgreSQL target", retryable: false}
		}
		return bootstrapCleanupError{message: "DROP PostgreSQL target не применён", retryable: true}
	}
	if err := setBootstrapTargetDropped(ctx, connection, marker.runID, target, snapshot.databaseOID); err != nil {
		return bootstrapCleanupError{message: "cleanup PostgreSQL target не зафиксировал подтверждённый DROP", retryable: true}
	}
	return nil
}

func openBootstrapMaintenance(
	ctx context.Context,
	bootstrapDSN string,
	target DisposableDatabase,
) (*pgxpool.Config, disposableMarker, *pgxpool.Pool, *pgxpool.Conn, postgresServerIdentity, error) {
	if target.Database == "" || target.Database != disposableDatabaseName(target.Marker) {
		return nil, disposableMarker{}, nil, nil, postgresServerIdentity{}, bootstrapCleanupError{
			message: "identity одноразовой PostgreSQL database не подтверждена", retryable: false,
		}
	}
	marker, err := parseDisposableMarker(target.Marker, time.Now().UTC())
	if err != nil {
		return nil, disposableMarker{}, nil, nil, postgresServerIdentity{}, bootstrapCleanupError{
			message: "bootstrap PostgreSQL identity не совпадает с target marker", retryable: false,
		}
	}
	config, err := validateBootstrapEndpointOffline(bootstrapDSN)
	if err != nil {
		return nil, disposableMarker{}, nil, nil, postgresServerIdentity{}, bootstrapCleanupError{
			message: "bootstrap PostgreSQL endpoint отклонён offline admission", retryable: false,
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, disposableMarker{}, nil, nil, postgresServerIdentity{}, bootstrapCleanupError{
			message: "bootstrap PostgreSQL endpoint недоступен при cleanup", retryable: true,
		}
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		return nil, disposableMarker{}, nil, nil, postgresServerIdentity{}, bootstrapCleanupError{
			message: "bootstrap PostgreSQL endpoint недоступен при cleanup", retryable: true,
		}
	}
	identity, err := readPostgresServerIdentity(ctx, connection, config)
	if err != nil {
		connection.Release()
		pool.Close()
		return nil, disposableMarker{}, nil, nil, postgresServerIdentity{}, bootstrapCleanupError{
			message: "bootstrap PostgreSQL server identity недоступна при cleanup", retryable: true,
		}
	}
	if identity.endpointFingerprint != marker.endpointFingerprint ||
		identity.serverFingerprint != marker.serverFingerprint || identity.currentDatabase == target.Database {
		connection.Release()
		pool.Close()
		return nil, disposableMarker{}, nil, nil, postgresServerIdentity{}, bootstrapCleanupError{
			message: "bootstrap PostgreSQL identity не совпадает с target marker", retryable: false,
		}
	}
	return config, marker, pool, connection, identity, nil
}

func loadBootstrapTargetLedger(
	ctx context.Context,
	querier rowQuerier,
	marker disposableMarker,
	target DisposableDatabase,
	identity postgresServerIdentity,
) (bootstrapTargetLedger, error) {
	var ledger bootstrapTargetLedger
	var version int
	var purpose string
	var consumed bool
	var consumedBy string
	query := fmt.Sprintf(`
select target_database, target_marker_sha256, target_owner_oid::bigint,
	coalesce(target_database_oid::bigint, 0), target_state, maintenance_database,
	endpoint_fingerprint, server_fingerprint, version, purpose,
	consumed_at is not null, coalesce(consumed_by, '')
from public.%s
where run_id = $1
`, pgx.Identifier{bootstrapProofTable}.Sanitize())
	if err := querier.QueryRow(ctx, query, marker.runID).Scan(
		&ledger.database, &ledger.markerSHA256, &ledger.ownerOID, &ledger.databaseOID,
		&ledger.state, &ledger.maintenanceDB, &ledger.endpointBinding, &ledger.serverBinding,
		&version, &purpose, &consumed, &consumedBy,
	); err != nil {
		return bootstrapTargetLedger{}, fmt.Errorf("exact consumed bootstrap claim отсутствует")
	}
	markerDigest := sha256.Sum256([]byte(target.Marker))
	if version != bootstrapProofVersion || purpose != bootstrapProofPurpose || !consumed || consumedBy != marker.runID ||
		ledger.database != target.Database || !equalBytes(ledger.markerSHA256, markerDigest[:]) || ledger.ownerOID <= 0 ||
		ledger.maintenanceDB != identity.currentDatabase || ledger.endpointBinding != identity.endpointFingerprint ||
		ledger.serverBinding != identity.serverFingerprint || ledger.ownerOID != identity.currentUserOID ||
		!validBootstrapTargetState(ledger.state) {
		return bootstrapTargetLedger{}, fmt.Errorf("exact consumed bootstrap claim имеет несовпадающую identity")
	}
	return ledger, nil
}

type bootstrapTargetSnapshot struct {
	exists        bool
	databaseOID   int64
	ownerOID      int64
	comment       string
	isTemplate    bool
	allowsConnect bool
}

func readBootstrapTargetSnapshot(ctx context.Context, querier rowQuerier, database string) (bootstrapTargetSnapshot, error) {
	var snapshot bootstrapTargetSnapshot
	err := querier.QueryRow(ctx, `
select oid::bigint, datdba::bigint, coalesce(shobj_description(oid, 'pg_database'), ''),
	datistemplate, datallowconn
from pg_database
where datname = $1
`, database).Scan(&snapshot.databaseOID, &snapshot.ownerOID, &snapshot.comment, &snapshot.isTemplate, &snapshot.allowsConnect)
	if errors.Is(err, pgx.ErrNoRows) {
		return bootstrapTargetSnapshot{}, nil
	}
	if err != nil {
		return bootstrapTargetSnapshot{}, err
	}
	snapshot.exists = true
	return snapshot, nil
}

func validateBootstrapTargetSnapshot(
	snapshot bootstrapTargetSnapshot,
	ledger bootstrapTargetLedger,
	markerValue string,
	requireMarker bool,
) error {
	if !snapshot.exists || snapshot.ownerOID != ledger.ownerOID || snapshot.isTemplate || !snapshot.allowsConnect {
		return fmt.Errorf("PostgreSQL target имеет несовпадающие owner или database attributes")
	}
	if ledger.databaseOID != 0 && snapshot.databaseOID != ledger.databaseOID {
		return fmt.Errorf("PostgreSQL target имеет несовпадающий creation OID")
	}
	if requireMarker && snapshot.comment != markerValue {
		return fmt.Errorf("PostgreSQL target не имеет exact applied marker")
	}
	if !requireMarker && snapshot.comment != "" && snapshot.comment != markerValue {
		return fmt.Errorf("PostgreSQL target имеет foreign marker")
	}
	return nil
}

func setBootstrapTargetCreated(
	ctx context.Context,
	querier rowQuerier,
	runID string,
	target DisposableDatabase,
	databaseOID int64,
) error {
	markerDigest := sha256.Sum256([]byte(target.Marker))
	var updated bool
	query := fmt.Sprintf(`
update public.%s set
	target_database_oid = $4::oid,
	target_state = $5,
	target_identified_at = clock_timestamp()
where run_id = $1
	and target_database = $2
	and target_marker_sha256 = $3
	and target_database_oid is null
	and target_state = $6
returning true
`, pgx.Identifier{bootstrapProofTable}.Sanitize())
	if err := querier.QueryRow(ctx, query, runID, target.Database, markerDigest[:], databaseOID,
		bootstrapTargetStateCreated, bootstrapTargetStateReserved,
	).Scan(&updated); err != nil || !updated {
		return fmt.Errorf("creation OID одноразовой PostgreSQL database не зафиксирован")
	}
	return nil
}

func setBootstrapTargetMarked(
	ctx context.Context,
	querier rowQuerier,
	runID string,
	target DisposableDatabase,
	databaseOID int64,
) error {
	markerDigest := sha256.Sum256([]byte(target.Marker))
	var updated bool
	query := fmt.Sprintf(`
update public.%s set
	target_state = $5,
	target_marker_applied_at = coalesce(target_marker_applied_at, clock_timestamp())
where run_id = $1
	and target_database = $2
	and target_marker_sha256 = $3
	and target_database_oid = $4::oid
	and target_state in ($6, $5)
returning true
`, pgx.Identifier{bootstrapProofTable}.Sanitize())
	if err := querier.QueryRow(ctx, query, runID, target.Database, markerDigest[:], databaseOID,
		bootstrapTargetStateMarked, bootstrapTargetStateCreated,
	).Scan(&updated); err != nil || !updated {
		return fmt.Errorf("applied marker одноразовой PostgreSQL database не зафиксирован")
	}
	return nil
}

func setBootstrapTargetDropped(
	ctx context.Context,
	querier rowQuerier,
	runID string,
	target DisposableDatabase,
	databaseOID int64,
) error {
	markerDigest := sha256.Sum256([]byte(target.Marker))
	var updated bool
	query := fmt.Sprintf(`
update public.%s set
	target_state = $5,
	target_dropped_at = coalesce(target_dropped_at, clock_timestamp())
where run_id = $1
	and target_database = $2
	and target_marker_sha256 = $3
	and (target_database_oid = nullif($4, 0)::oid or (target_database_oid is null and $4 = 0))
	and target_state in ($6, $7, $8, $5)
returning true
`, pgx.Identifier{bootstrapProofTable}.Sanitize())
	if err := querier.QueryRow(ctx, query, runID, target.Database, markerDigest[:], databaseOID,
		bootstrapTargetStateDropped, bootstrapTargetStateReserved, bootstrapTargetStateCreated, bootstrapTargetStateMarked,
	).Scan(&updated); err != nil || !updated {
		return fmt.Errorf("подтверждённое удаление одноразовой PostgreSQL database не зафиксировано")
	}
	return nil
}

func validBootstrapTargetState(state string) bool {
	return state == bootstrapTargetStateReserved || state == bootstrapTargetStateCreated ||
		state == bootstrapTargetStateMarked || state == bootstrapTargetStateDropped
}

func bootstrapLifecycleOptionsFromContext(ctx context.Context) bootstrapLifecycleOptions {
	options, _ := ctx.Value(bootstrapLifecycleOptionsContextKey{}).(bootstrapLifecycleOptions)
	if options.cleanupTimeout <= 0 {
		options.cleanupTimeout = bootstrapCleanupTimeout
	}
	if options.attemptTimeout <= 0 {
		options.attemptTimeout = bootstrapCleanupAttempt
	}
	if options.retryDelay <= 0 {
		options.retryDelay = bootstrapCleanupRetryDelay
	}
	if options.attempts <= 0 {
		options.attempts = bootstrapCleanupAttempts
	}
	return options
}

func runBootstrapLifecycleHook(
	ctx context.Context,
	point bootstrapLifecycleHookPoint,
	input bootstrapLifecycleHookInput,
) error {
	options := bootstrapLifecycleOptionsFromContext(ctx)
	if options.hook == nil {
		return nil
	}
	return options.hook(ctx, point, input)
}

func validateDisposableIdentityOffline(dsn string, markerValue string, now time.Time) (*pgxpool.Config, disposableMarker, error) {
	config, err := parseDSNWithoutDisclosure(dsn)
	if err != nil {
		return nil, disposableMarker{}, err
	}
	marker, err := parseDisposableMarker(markerValue, now)
	if err != nil {
		return nil, disposableMarker{}, err
	}
	database := strings.TrimSpace(config.ConnConfig.Database)
	if database == "" || database != disposableDatabaseName(markerValue) || canonicalDatabaseName(database) {
		return nil, disposableMarker{}, fmt.Errorf("PostgreSQL target не является одноразовой database")
	}
	if err := admitConfigEndpoints(config); err != nil {
		return nil, disposableMarker{}, err
	}
	if configuredProductionIdentity(config, database) {
		return nil, disposableMarker{}, fmt.Errorf("PostgreSQL target совпадает с настроенной production identity")
	}
	return config, marker, nil
}

func validateBootstrapEndpointOffline(dsn string) (*pgxpool.Config, error) {
	config, err := parseDSNWithoutDisclosure(dsn)
	if err != nil {
		return nil, err
	}
	if err := admitConfigEndpoints(config); err != nil {
		return nil, err
	}
	if configuredProductionIdentity(config, config.ConnConfig.Database) {
		return nil, fmt.Errorf("bootstrap PostgreSQL endpoint совпадает с настроенной production identity")
	}
	return config, nil
}

func claimBootstrapProof(
	ctx context.Context,
	querier rowQuerier,
	config *pgxpool.Config,
	proof parsedBootstrapProof,
	target DisposableDatabase,
) (postgresServerIdentity, error) {
	if err := validateBootstrapProofRegistry(ctx, querier); err != nil {
		return postgresServerIdentity{}, err
	}
	identity, err := readPostgresServerIdentity(ctx, querier, config)
	if err != nil {
		return postgresServerIdentity{}, err
	}
	if identity.currentDatabase != proof.MaintenanceDatabase ||
		identity.endpointFingerprint != proof.EndpointFingerprint ||
		identity.serverFingerprint != proof.ServerFingerprint {
		return postgresServerIdentity{}, fmt.Errorf("bootstrap PostgreSQL proof не соответствует exact server identity")
	}
	markerDigest := sha256.Sum256([]byte(target.Marker))
	var claimed bool
	claimQuery := fmt.Sprintf(`
update public.%s set
	consumed_at = clock_timestamp(),
	consumed_by = $2,
	target_database = $11,
	target_marker_sha256 = $12,
	target_owner_oid = $13,
	target_state = $14,
	target_reserved_at = clock_timestamp()
where nonce_sha256 = $1
	and version = $3
	and issued_at = $4
	and expires_at = $5
	and endpoint_fingerprint = $6
	and server_fingerprint = $7
	and maintenance_database = $8
	and purpose = $9
	and run_id = $10
	and consumed_at is null
	and target_database is null
	and target_marker_sha256 is null
	and target_owner_oid is null
	and target_database_oid is null
	and target_state is null
	and clock_timestamp() >= issued_at - interval '30 seconds'
	and clock_timestamp() < expires_at
returning true
`, pgx.Identifier{bootstrapProofTable}.Sanitize())
	if err := querier.QueryRow(ctx, claimQuery,
		proof.nonceHash, proof.RunID, proof.Version, proof.issuedAt, proof.expiresAt,
		proof.EndpointFingerprint, proof.ServerFingerprint, proof.MaintenanceDatabase,
		proof.Purpose, proof.RunID, target.Database, markerDigest[:], identity.currentUserOID,
		bootstrapTargetStateReserved,
	).Scan(&claimed); err != nil || !claimed {
		return postgresServerIdentity{}, fmt.Errorf("bootstrap PostgreSQL proof отсутствует, истёк или уже использован")
	}
	return identity, nil
}

func validateBootstrapProofRegistry(ctx context.Context, querier rowQuerier) error {
	var columns []string
	if err := querier.QueryRow(ctx, `
select array_agg(column_name || ':' || udt_name || ':' || is_nullable order by ordinal_position)
from information_schema.columns
where table_schema = 'public' and table_name = $1
`, bootstrapProofTable).Scan(&columns); err != nil || strings.Join(columns, ",") != strings.Join(bootstrapProofRegistryColumns(), ",") {
		return fmt.Errorf("bootstrap PostgreSQL proof registry не соответствует exact lifecycle contract")
	}
	var exactGuard bool
	if err := querier.QueryRow(ctx, `
select count(*) = 1 and bool_and(trigger_row.tgenabled = 'O')
from pg_trigger trigger_row
join pg_class table_row on table_row.oid = trigger_row.tgrelid
join pg_namespace namespace_row on namespace_row.oid = table_row.relnamespace
join pg_proc function_row on function_row.oid = trigger_row.tgfoid
where namespace_row.nspname = 'public'
	and table_row.relname = $1
	and trigger_row.tgname = $2
	and function_row.proname = $3
	and not trigger_row.tgisinternal
`, bootstrapProofTable, bootstrapProofGuardTrigger, bootstrapProofGuardFunction).Scan(&exactGuard); err != nil || !exactGuard {
		return fmt.Errorf("bootstrap PostgreSQL proof registry не имеет immutable creation ledger guard")
	}
	return nil
}

func bootstrapProofRegistryColumns() []string {
	return []string{
		"nonce_sha256:bytea:NO",
		"version:int4:NO",
		"issued_at:timestamptz:NO",
		"expires_at:timestamptz:NO",
		"endpoint_fingerprint:text:NO",
		"server_fingerprint:text:NO",
		"maintenance_database:text:NO",
		"purpose:text:NO",
		"run_id:text:NO",
		"consumed_at:timestamptz:YES",
		"consumed_by:text:YES",
		"target_database:text:YES",
		"target_marker_sha256:bytea:YES",
		"target_owner_oid:oid:YES",
		"target_database_oid:oid:YES",
		"target_state:text:YES",
		"target_reserved_at:timestamptz:YES",
		"target_identified_at:timestamptz:YES",
		"target_marker_applied_at:timestamptz:YES",
		"target_dropped_at:timestamptz:YES",
	}
}

func parseBootstrapProof(value string, now time.Time) (parsedBootstrapProof, error) {
	if strings.TrimSpace(value) == "" {
		return parsedBootstrapProof{}, fmt.Errorf("bootstrap PostgreSQL proof отсутствует")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	var proof bootstrapProof
	if err := decoder.Decode(&proof); err != nil {
		return parsedBootstrapProof{}, fmt.Errorf("bootstrap PostgreSQL proof имеет недопустимую структуру")
	}
	if decoder.Decode(&struct{}{}) == nil {
		return parsedBootstrapProof{}, fmt.Errorf("bootstrap PostgreSQL proof имеет недопустимую структуру")
	}
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, proof.IssuedAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, proof.ExpiresAt)
	nonce, nonceErr := hex.DecodeString(proof.Nonce)
	nonceHash, hashErr := hex.DecodeString(proof.NonceSHA256)
	computedHash := sha256.Sum256(nonce)
	if issuedErr != nil || expiresErr != nil || nonceErr != nil || hashErr != nil || len(nonce) != 32 || len(nonceHash) != sha256.Size || !equalBytes(nonceHash, computedHash[:]) ||
		proof.Version != bootstrapProofVersion || proof.Purpose != bootstrapProofPurpose || proof.State != bootstrapProofState ||
		!validSHA256Hex(proof.EndpointFingerprint) || !validSHA256Hex(proof.ServerFingerprint) || !validHexIdentifier(proof.RunID, 32) || strings.TrimSpace(proof.MaintenanceDatabase) == "" {
		return parsedBootstrapProof{}, fmt.Errorf("bootstrap PostgreSQL proof имеет недопустимые поля")
	}
	issuedAt = issuedAt.UTC()
	expiresAt = expiresAt.UTC()
	if issuedAt.After(now.Add(bootstrapProofClockSkew)) || !expiresAt.After(now) || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > bootstrapProofLifetime {
		return parsedBootstrapProof{}, fmt.Errorf("bootstrap PostgreSQL proof просрочен или ещё не действителен")
	}
	return parsedBootstrapProof{bootstrapProof: proof, issuedAt: issuedAt, expiresAt: expiresAt, nonceHash: nonceHash}, nil
}

func validSHA256Hex(value string) bool {
	return validHexIdentifier(value, sha256.Size*2)
}

func validHexIdentifier(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func equalBytes(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func parseDSNWithoutDisclosure(dsn string) (*pgxpool.Config, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("PostgreSQL DSN отсутствует")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL DSN имеет недопустимый URL/keyword синтаксис")
	}
	return config, nil
}

func admitConfigEndpoints(config *pgxpool.Config) error {
	if err := admitEndpoint(config.ConnConfig.Host, config.ConnConfig.Port); err != nil {
		return err
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(fallback.Host), strings.TrimSpace(config.ConnConfig.Host)) || fallback.Port != config.ConnConfig.Port {
			return fmt.Errorf("PostgreSQL DSN с альтернативным fallback endpoint запрещён")
		}
	}
	return nil
}

func admitEndpoint(host string, port uint16) error {
	host = strings.TrimSpace(host)
	if localEndpoint(host) {
		return nil
	}
	endpoint := net.JoinHostPort(host, strconv.Itoa(int(port)))
	for _, allowed := range strings.Split(os.Getenv("MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINTS"), ",") {
		if strings.EqualFold(strings.TrimSpace(allowed), endpoint) && host != "" {
			return nil
		}
	}
	return fmt.Errorf("PostgreSQL endpoint не входит в fail-closed ephemeral allowlist")
}

func localEndpoint(host string) bool {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "/") {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func configuredProductionIdentity(config *pgxpool.Config, database string) bool {
	database = strings.TrimSpace(database)
	hosts := []string{strings.TrimSpace(config.ConnConfig.Host)}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback != nil {
			hosts = append(hosts, strings.TrimSpace(fallback.Host))
		}
	}
	if configuredHost := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_HOST")); configuredHost != "" && containsFold(hosts, configuredHost) {
		return true
	}
	if configuredDatabase := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_DB")); configuredDatabase != "" && strings.EqualFold(configuredDatabase, database) {
		return true
	}
	for _, key := range []string{"MATTERCODEX_DATABASE_DSN", "MATTERCODEX_MIGRATIONS_DATABASE_DSN"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		config, err := pgxpool.ParseConfig(value)
		if err != nil {
			return true
		}
		productionHosts := []string{strings.TrimSpace(config.ConnConfig.Host)}
		for _, fallback := range config.ConnConfig.Fallbacks {
			if fallback != nil {
				productionHosts = append(productionHosts, strings.TrimSpace(fallback.Host))
			}
		}
		if intersectsFold(hosts, productionHosts) || strings.EqualFold(strings.TrimSpace(config.ConnConfig.Database), database) {
			return true
		}
	}
	return false
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if sameEndpointHost(value, expected) {
			return true
		}
	}
	return false
}

func sameEndpointHost(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return strings.EqualFold(left, right) || localEndpoint(left) && localEndpoint(right)
}

func intersectsFold(left []string, right []string) bool {
	for _, value := range left {
		if value != "" && containsFold(right, value) {
			return true
		}
	}
	return false
}

func canonicalDatabaseName(database string) bool {
	database = strings.ToLower(strings.TrimSpace(database))
	return database == "" || database == "postgres" || database == "mattermost" ||
		database == "mattercodex" || database == "matter_codex" || strings.HasPrefix(database, "template")
}

func newDisposableMarker(now time.Time, identity ...string) (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("генерация identity одноразовой PostgreSQL database не выполнена")
	}
	endpointFingerprint := strings.Repeat("0", sha256.Size*2)
	serverFingerprint := strings.Repeat("0", sha256.Size*2)
	runID := strings.Repeat("0", 32)
	if len(identity) != 0 {
		if len(identity) != 3 || !validSHA256Hex(identity[0]) || !validSHA256Hex(identity[1]) || !validHexIdentifier(identity[2], 32) {
			return "", fmt.Errorf("генерация identity одноразовой PostgreSQL database получила недопустимую server binding")
		}
		endpointFingerprint, serverFingerprint, runID = identity[0], identity[1], identity[2]
	}
	return strings.Join([]string{
		disposableMarkerPrefix,
		strconv.FormatInt(now.Unix(), 10),
		strconv.FormatInt(now.Add(disposableMarkerLifetime).Unix(), 10),
		hex.EncodeToString(token),
		endpointFingerprint,
		serverFingerprint,
		runID,
	}, ":"), nil
}

func parseDisposableMarker(value string, now time.Time) (disposableMarker, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 7 || parts[0] != disposableMarkerPrefix || len(parts[3]) != 64 ||
		!validSHA256Hex(parts[4]) || !validSHA256Hex(parts[5]) || !validHexIdentifier(parts[6], 32) {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker имеет недопустимый формат")
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker имеет недопустимый формат")
	}
	issuedUnix, issuedErr := strconv.ParseInt(parts[1], 10, 64)
	expiresUnix, expiresErr := strconv.ParseInt(parts[2], 10, 64)
	if issuedErr != nil || expiresErr != nil {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker имеет недопустимый срок")
	}
	marker := disposableMarker{
		issuedAt: time.Unix(issuedUnix, 0).UTC(), expiresAt: time.Unix(expiresUnix, 0).UTC(), token: parts[3],
		endpointFingerprint: parts[4], serverFingerprint: parts[5], runID: parts[6],
	}
	if marker.issuedAt.After(now.Add(disposableMarkerClockSkew)) || !marker.expiresAt.After(now) || marker.expiresAt.After(marker.issuedAt.Add(disposableMarkerLifetime+disposableMarkerClockSkew)) {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker просрочен или ещё не действителен")
	}
	return marker, nil
}

func disposableDatabaseName(markerValue string) string {
	digest := sha256.Sum256([]byte(markerValue))
	return disposableDatabasePrefix + hex.EncodeToString(digest[:12])
}

func markerForDSN(dsn string) (string, error) {
	config, err := parseDSNWithoutDisclosure(dsn)
	if err != nil {
		return "", err
	}
	database := strings.TrimSpace(config.ConnConfig.Database)
	if marker, ok := disposableMarkers.Load(database); ok {
		return marker.(string), nil
	}
	marker := strings.TrimSpace(os.Getenv(testDatabaseMarkerEnvironment))
	if marker == "" {
		return "", fmt.Errorf("per-run PostgreSQL marker отсутствует")
	}
	return marker, nil
}

func IsolatedSchemaDSN(t *testing.T, label string) string {
	t.Helper()
	baseDSN := RequiredDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := EnsureVectorExtension(ctx, baseDSN); err != nil {
		t.Fatalf("подготовка database-global extension vector: %v", err)
	}
	schema := uniqueName("mc_"+label, 48)
	markerValue, err := markerForDSN(baseDSN)
	if err != nil {
		t.Fatal("создание schema отклонено: disposable marker отсутствует")
	}
	config, marker, err := validateDisposableIdentityOffline(baseDSN, markerValue, time.Now().UTC())
	if err != nil {
		t.Fatal("создание schema отклонено offline admission")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("создание подключения к PostgreSQL не выполнено")
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatal("подключение для создания schema не получено")
	}
	if err := validateDisposableDatabaseConnection(ctx, connection, config, markerValue, marker); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("CREATE SCHEMA отклонён повторным admission одноразовой database")
	}
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := connection.Exec(ctx, "create schema "+identifier); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание изолированной схемы PostgreSQL не выполнено")
	}
	connection.Release()
	pool.Close()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		marker, markerErr := markerForDSN(baseDSN)
		if markerErr != nil || ValidateDisposableDatabase(cleanupCtx, baseDSN, marker) != nil {
			t.Error("очистка schema отклонена повторным admission одноразовой database")
			return
		}
		cleanupConfig, cleanupMarker, cleanupErr := validateDisposableIdentityOffline(baseDSN, marker, time.Now().UTC())
		if cleanupErr != nil {
			t.Error("очистка schema отклонена offline admission")
			return
		}
		cleanupPool, cleanupErr := pgxpool.NewWithConfig(cleanupCtx, cleanupConfig)
		if cleanupErr != nil {
			t.Error("подключение для очистки PostgreSQL не выполнено")
			return
		}
		defer cleanupPool.Close()
		cleanupConnection, cleanupErr := cleanupPool.Acquire(cleanupCtx)
		if cleanupErr != nil {
			t.Error("подключение для очистки schema не получено")
			return
		}
		defer cleanupConnection.Release()
		if validateDisposableDatabaseConnection(cleanupCtx, cleanupConnection, cleanupConfig, marker, cleanupMarker) != nil {
			t.Error("DROP SCHEMA отклонён повторным admission одноразовой database")
			return
		}
		if _, cleanupErr := cleanupConnection.Exec(cleanupCtx, "drop schema "+identifier+" cascade"); cleanupErr != nil {
			t.Error("очистка схемы PostgreSQL не выполнена")
		}
	})
	isolatedDSN := dsnWithDatabaseAndSearchPath(t, baseDSN, "", schema+",public")
	if err := ValidateDisposableDatabase(ctx, isolatedDSN, markerValue); err != nil {
		t.Fatal("изолированная schema не прошла финальный read-only admission перед DDL теста")
	}
	return isolatedDSN
}

func EnsureVectorExtension(ctx context.Context, dsn string) error {
	marker, err := markerForDSN(dsn)
	if err != nil {
		return fmt.Errorf("CREATE EXTENSION отклонён admission одноразовой PostgreSQL database")
	}
	config, parsedMarker, err := validateDisposableIdentityOffline(dsn, marker, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("CREATE EXTENSION отклонён admission одноразовой PostgreSQL database")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("создание подключения для extension не выполнено")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции extension: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := validateDisposableDatabaseConnection(ctx, tx, config, marker, parsedMarker); err != nil {
		return fmt.Errorf("CREATE EXTENSION отклонён повторным admission одноразовой PostgreSQL database")
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, vectorExtensionLockName); err != nil {
		return fmt.Errorf("database-global advisory lock: %w", err)
	}
	if _, err := tx.Exec(ctx, `create extension if not exists vector with schema public`); err != nil {
		return fmt.Errorf("CREATE EXTENSION vector: %w", err)
	}
	var extensionCount int
	var typeAvailable bool
	if err := tx.QueryRow(ctx, `
select count(*), to_regtype('public.vector') is not null
from pg_extension extension_row
join pg_namespace namespace_row on namespace_row.oid = extension_row.extnamespace
where extension_row.extname = 'vector' and namespace_row.nspname = 'public'
`).Scan(&extensionCount, &typeAvailable); err != nil {
		return fmt.Errorf("проверка extension vector: %w", err)
	}
	if extensionCount != 1 || !typeAvailable {
		return fmt.Errorf("extension vector должна быть единственной в public: count=%d type_available=%t", extensionCount, typeAvailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("фиксация extension vector: %w", err)
	}
	return nil
}

func FreshDatabaseDSN(t *testing.T, label string) string {
	t.Helper()
	baseDSN := RequiredDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	baseMarkerValue, err := markerForDSN(baseDSN)
	if err != nil {
		t.Fatal("parent PostgreSQL database не имеет disposable identity")
	}
	baseConfig, baseMarker, err := validateDisposableIdentityOffline(baseDSN, baseMarkerValue, time.Now().UTC())
	if err != nil {
		t.Fatal("parent PostgreSQL database не прошла offline admission")
	}
	pool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal("подключение для создания тестовой database не выполнено")
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatal("подключение для создания тестовой database не получено")
	}
	if err := validateDisposableDatabaseConnection(ctx, connection, baseConfig, baseMarkerValue, baseMarker); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание database отклонено повторным admission parent database")
	}
	identity, err := readPostgresServerIdentity(ctx, connection, baseConfig)
	if err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание database отклонено server identity")
	}
	if err := validateGeneratedPrivateClusterAuthority(ctx, baseConfig, identity); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание чистой database требует generated private-cluster authority")
	}
	runID, err := randomHex(16)
	if err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("генерация run identity чистой PostgreSQL database не выполнена")
	}
	markerValue, err := newDisposableMarker(time.Now().UTC(), identity.endpointFingerprint, identity.serverFingerprint, runID)
	if err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("генерация identity чистой PostgreSQL database не выполнена")
	}
	database := disposableDatabaseName(markerValue)
	identifier := pgx.Identifier{database}.Sanitize()
	if _, err := connection.Exec(ctx, "create database "+identifier+" template template0"); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание чистой одноразовой database не выполнено")
	}
	comment := strings.ReplaceAll(markerValue, "'", "''")
	if _, err := connection.Exec(ctx, "comment on database "+identifier+" is '"+comment+"'"); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("маркировка чистой одноразовой PostgreSQL database не выполнена")
	}
	connection.Release()
	pool.Close()
	targetDSN := dsnWithDatabaseAndSearchPath(t, baseDSN, database, "public")
	disposableMarkers.Store(database, markerValue)
	if err := ValidateDisposableDatabase(ctx, targetDSN, markerValue); err != nil {
		t.Fatal("чистая одноразовая PostgreSQL database не прошла admission")
	}
	t.Cleanup(func() {
		disposableMarkers.Delete(database)
	})
	return targetDSN
}

func randomHex(byteCount int) (string, error) {
	value := make([]byte, byteCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// provisionGeneratedBootstrapProof вызывается только harness после offline-init
// принадлежащего ему PGDATA. Произвольный внешний DSN через этот путь не принимается.
func provisionGeneratedBootstrapProof(
	ctx context.Context,
	bootstrapDSN string,
	expectedDataDirectory string,
	expectedSocketDirectory string,
) (string, error) {
	config, err := parseDSNWithoutDisclosure(bootstrapDSN)
	if err != nil {
		return "", err
	}
	configuredEndpoint := strings.TrimSpace(config.ConnConfig.Host)
	expectedSocket, err := filepath.EvalSymlinks(expectedSocketDirectory)
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не подтвердил socket endpoint")
	}
	if strings.HasPrefix(configuredEndpoint, "/") {
		configuredHost, hostErr := filepath.EvalSymlinks(configuredEndpoint)
		if hostErr != nil || configuredHost != expectedSocket {
			return "", fmt.Errorf("generated PostgreSQL harness не владеет socket endpoint")
		}
	} else if !localEndpoint(configuredEndpoint) {
		return "", fmt.Errorf("generated PostgreSQL harness не владеет loopback endpoint")
	}
	expectedData, err := filepath.EvalSymlinks(expectedDataDirectory)
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не подтвердил PGDATA")
	}
	dataInfo, err := os.Stat(expectedData)
	if err != nil || !dataInfo.IsDir() || dataInfo.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("generated PostgreSQL harness не владеет приватным PGDATA")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не подключился к bootstrap database")
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не получил bootstrap connection")
	}
	defer connection.Release()
	identity, err := readPostgresServerIdentity(ctx, connection, config)
	if err != nil {
		return "", err
	}
	actualData, err := filepath.EvalSymlinks(identity.dataDirectory)
	if err != nil || actualData != expectedData || identity.currentDatabase != "postgres" {
		return "", fmt.Errorf("generated PostgreSQL harness получил несовпадающую server identity")
	}
	if err := validateBootstrapProofRegistry(ctx, connection); err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не нашёл exact proof registry")
	}
	nonce, err := randomHex(32)
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не создал proof nonce")
	}
	nonceBytes, _ := hex.DecodeString(nonce)
	nonceDigest := sha256.Sum256(nonceBytes)
	runID, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не создал proof run identity")
	}
	issuedAt := time.Now().UTC().Truncate(time.Microsecond)
	expiresAt := issuedAt.Add(bootstrapProofLifetime)
	proof := bootstrapProof{
		Version: bootstrapProofVersion, Nonce: nonce, NonceSHA256: hex.EncodeToString(nonceDigest[:]),
		IssuedAt: issuedAt.Format(time.RFC3339Nano), ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		EndpointFingerprint: identity.endpointFingerprint, ServerFingerprint: identity.serverFingerprint,
		MaintenanceDatabase: identity.currentDatabase, Purpose: bootstrapProofPurpose,
		RunID: runID, State: bootstrapProofState,
	}
	encoded, err := json.Marshal(proof)
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не сериализовал proof")
	}
	table := pgx.Identifier{bootstrapProofTable}.Sanitize()
	if _, err := connection.Exec(ctx, fmt.Sprintf(`
insert into public.%s(
	nonce_sha256, version, issued_at, expires_at, endpoint_fingerprint,
	server_fingerprint, maintenance_database, purpose, run_id
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`, table), nonceDigest[:], proof.Version, issuedAt, expiresAt, proof.EndpointFingerprint,
		proof.ServerFingerprint, proof.MaintenanceDatabase, proof.Purpose, proof.RunID,
	); err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не зарегистрировал one-shot proof")
	}
	return string(encoded), nil
}

func dsnWithDatabaseAndSearchPath(t *testing.T, dsn string, database string, searchPath string) string {
	t.Helper()
	result, err := deriveDSN(dsn, database, searchPath)
	if err != nil {
		t.Fatal("не удалось получить DSN одноразовой PostgreSQL database")
	}
	return result
}

func deriveDSN(dsn string, database string, searchPath string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		if database != "" {
			parsed.Path = "/" + database
		}
		query := parsed.Query()
		query.Set("search_path", searchPath)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	if strings.ContainsAny(database+searchPath, " '\"=\\") {
		return "", fmt.Errorf("небезопасный идентификатор одноразовой PostgreSQL")
	}
	result := strings.TrimSpace(dsn)
	if database != "" {
		result += " dbname=" + database
	}
	return result + " search_path=" + searchPath, nil
}

func uniqueName(prefix string, maximumLength int) string {
	suffix := fmt.Sprintf("_%x_%x", time.Now().UTC().UnixNano(), resourceSequence.Add(1))
	prefix = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			return character
		}
		return '_'
	}, strings.ToLower(prefix))
	if len(prefix)+len(suffix) > maximumLength {
		prefix = prefix[:maximumLength-len(suffix)]
	}
	return prefix + suffix
}
