// Package testsupport предоставляет единый безопасный lifecycle одноразовой PostgreSQL для тестов.
package testsupport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	testDatabaseDSNEnvironment    = "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"
	testDatabaseMarkerEnvironment = "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER"
	testBootstrapProofEnvironment = "MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF"
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
	endpointFingerprint string
	serverFingerprint   string
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
select current_database(), coalesce(inet_server_addr()::text, ''), coalesce(inet_server_port(), 0),
	current_setting('data_directory'), system_identifier::text
from pg_control_system()
`).Scan(
		&identity.currentDatabase, &identity.serverAddress, &identity.serverPort,
		&identity.dataDirectory, &identity.systemIdentifier,
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

func BootstrapDisposableDatabase(ctx context.Context, bootstrapDSN string, proofValue string) (DisposableDatabase, error) {
	config, err := validateBootstrapEndpointOffline(bootstrapDSN)
	if err != nil {
		return DisposableDatabase{}, err
	}
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
	identity, proof, err := claimBootstrapProof(ctx, connection, config, proofValue, time.Now().UTC())
	if err != nil {
		return DisposableDatabase{}, err
	}
	markerValue, err := newDisposableMarker(
		time.Now().UTC(), identity.endpointFingerprint, identity.serverFingerprint, proof.RunID,
	)
	if err != nil {
		return DisposableDatabase{}, err
	}
	database := disposableDatabaseName(markerValue)
	identifier := pgx.Identifier{database}.Sanitize()
	if _, err := connection.Exec(ctx, "create database "+identifier+" template template0"); err != nil {
		return DisposableDatabase{}, fmt.Errorf("создание одноразовой PostgreSQL database не выполнено")
	}
	comment := strings.ReplaceAll(markerValue, "'", "''")
	if _, err := connection.Exec(ctx, "comment on database "+identifier+" is '"+comment+"'"); err != nil {
		return DisposableDatabase{}, fmt.Errorf("маркировка одноразовой PostgreSQL database не выполнена")
	}
	targetDSN, err := deriveDSN(bootstrapDSN, database, "public")
	if err != nil {
		return DisposableDatabase{}, err
	}
	disposableMarkers.Store(database, markerValue)
	if err := ValidateDisposableDatabase(ctx, targetDSN, markerValue); err != nil {
		return DisposableDatabase{}, err
	}
	return DisposableDatabase{DSN: targetDSN, Marker: markerValue, Database: database}, nil
}

func DestroyDisposableDatabase(ctx context.Context, bootstrapDSN string, target DisposableDatabase) error {
	if target.Database == "" || target.Database != disposableDatabaseName(target.Marker) {
		return fmt.Errorf("identity одноразовой PostgreSQL database не подтверждена")
	}
	if err := ValidateDisposableDatabase(ctx, target.DSN, target.Marker); err != nil {
		return fmt.Errorf("удаление PostgreSQL target отклонено повторным admission")
	}
	config, err := validateBootstrapEndpointOffline(bootstrapDSN)
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap PostgreSQL endpoint недоступен при cleanup")
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap PostgreSQL endpoint недоступен при cleanup")
	}
	defer connection.Release()
	marker, err := parseDisposableMarker(target.Marker, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("bootstrap PostgreSQL identity не совпадает с target marker")
	}
	identity, err := readPostgresServerIdentity(ctx, connection, config)
	if err != nil || identity.endpointFingerprint != marker.endpointFingerprint || identity.serverFingerprint != marker.serverFingerprint {
		return fmt.Errorf("bootstrap PostgreSQL identity не совпадает с target marker")
	}
	if target.Database != disposableDatabaseName(target.Marker) {
		return fmt.Errorf("удаление PostgreSQL target отклонено точной identity")
	}
	if err := ValidateDisposableDatabase(ctx, target.DSN, target.Marker); err != nil {
		return fmt.Errorf("удаление PostgreSQL target отклонено финальным admission")
	}
	if _, err := connection.Exec(ctx, "drop database "+pgx.Identifier{target.Database}.Sanitize()+" with (force)"); err != nil {
		return fmt.Errorf("удаление одноразовой PostgreSQL database не выполнено")
	}
	disposableMarkers.Delete(target.Database)
	return nil
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

func claimBootstrapProof(ctx context.Context, querier rowQuerier, config *pgxpool.Config, proofValue string, now time.Time) (postgresServerIdentity, parsedBootstrapProof, error) {
	proof, err := parseBootstrapProof(proofValue, now)
	if err != nil {
		return postgresServerIdentity{}, parsedBootstrapProof{}, err
	}
	identity, err := readPostgresServerIdentity(ctx, querier, config)
	if err != nil {
		return postgresServerIdentity{}, parsedBootstrapProof{}, err
	}
	if identity.currentDatabase != proof.MaintenanceDatabase ||
		identity.endpointFingerprint != proof.EndpointFingerprint ||
		identity.serverFingerprint != proof.ServerFingerprint {
		return postgresServerIdentity{}, parsedBootstrapProof{}, fmt.Errorf("bootstrap PostgreSQL proof не соответствует exact server identity")
	}
	var claimed bool
	claimQuery := fmt.Sprintf(`
update public.%s set
	consumed_at = clock_timestamp(),
	consumed_by = $2
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
	and clock_timestamp() >= issued_at - interval '30 seconds'
	and clock_timestamp() < expires_at
returning true
`, pgx.Identifier{bootstrapProofTable}.Sanitize())
	if err := querier.QueryRow(ctx, claimQuery,
		proof.nonceHash, proof.RunID, proof.Version, proof.issuedAt, proof.expiresAt,
		proof.EndpointFingerprint, proof.ServerFingerprint, proof.MaintenanceDatabase,
		proof.Purpose, proof.RunID,
	).Scan(&claimed); err != nil || !claimed {
		return postgresServerIdentity{}, parsedBootstrapProof{}, fmt.Errorf("bootstrap PostgreSQL proof отсутствует, истёк или уже использован")
	}
	return identity, proof, nil
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if ValidateDisposableDatabase(cleanupCtx, targetDSN, markerValue) != nil {
			t.Error("удаление чистой PostgreSQL database отклонено повторным admission")
			return
		}
		parentMarker, markerErr := markerForDSN(baseDSN)
		if markerErr != nil || ValidateDisposableDatabase(cleanupCtx, baseDSN, parentMarker) != nil {
			t.Error("cleanup parent PostgreSQL database не прошла admission")
			return
		}
		cleanupConfig, cleanupMarker, cleanupErr := validateDisposableIdentityOffline(baseDSN, parentMarker, time.Now().UTC())
		if cleanupErr != nil {
			t.Error("cleanup parent PostgreSQL database не прошла offline admission")
			return
		}
		cleanupPool, cleanupErr := pgxpool.NewWithConfig(cleanupCtx, cleanupConfig)
		if cleanupErr != nil {
			t.Error("подключение для удаления тестовой database не выполнено")
			return
		}
		defer cleanupPool.Close()
		cleanupConnection, cleanupErr := cleanupPool.Acquire(cleanupCtx)
		if cleanupErr != nil {
			t.Error("cleanup connection одноразовой database не получено")
			return
		}
		defer cleanupConnection.Release()
		if validateDisposableDatabaseConnection(cleanupCtx, cleanupConnection, cleanupConfig, parentMarker, cleanupMarker) != nil {
			t.Error("DROP DATABASE отклонён повторным admission parent database")
			return
		}
		if _, cleanupErr := cleanupConnection.Exec(cleanupCtx, "drop database "+identifier+" with (force)"); cleanupErr != nil {
			t.Error("удаление одноразовой database не выполнено")
		}
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
	configuredHost, err := filepath.EvalSymlinks(strings.TrimSpace(config.ConnConfig.Host))
	if err != nil {
		return "", fmt.Errorf("generated PostgreSQL harness не подтвердил socket endpoint")
	}
	expectedSocket, err := filepath.EvalSymlinks(expectedSocketDirectory)
	if err != nil || configuredHost != expectedSocket {
		return "", fmt.Errorf("generated PostgreSQL harness не владеет socket endpoint")
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
	var registryColumns []string
	if err := connection.QueryRow(ctx, `
select array_agg(column_name || ':' || udt_name || ':' || is_nullable order by ordinal_position)
from information_schema.columns
where table_schema = 'public' and table_name = $1
`, bootstrapProofTable).Scan(&registryColumns); err != nil || strings.Join(registryColumns, ",") != strings.Join([]string{
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
	}, ",") {
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
