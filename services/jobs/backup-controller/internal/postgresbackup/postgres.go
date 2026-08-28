// Package postgresbackup управляет консистентными logical dump и isolated restore.
package postgresbackup

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/backup__export_snapshot.sql
	exportSnapshotSQL string
	//go:embed sql/backup__server_version.sql
	serverVersionSQL string
	//go:embed sql/goose_schema__current_version.sql
	gooseVersionSQL string
	//go:embed sql/restore__database_exists.sql
	databaseExistsSQL string
)

type Snapshot struct {
	Name, SchemaKind, SchemaVersion, ServerVersion string
	StartedAt, FinishedAt                          time.Time
	DumpPath, DumpDigest                           string
	DumpSize                                       int64
	SchemaPath, SchemaDigest                       string
	SchemaSize                                     int64
}

type RestoreReadback struct {
	Name, SchemaVersion, TargetDigest string
}

type Manager struct {
	pgDump, pgRestore, createDB string
	maximumDatabaseBytes        int64
}

func New(maximumDatabaseBytes int64) (*Manager, error) {
	if maximumDatabaseBytes < 1<<20 {
		return nil, errors.New("maximum database backup size is invalid")
	}
	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		return nil, errors.New("pg_dump is unavailable")
	}
	pgRestore, err := exec.LookPath("pg_restore")
	if err != nil {
		return nil, errors.New("pg_restore is unavailable")
	}
	createDB, err := exec.LookPath("createdb")
	if err != nil {
		return nil, errors.New("createdb is unavailable")
	}
	return &Manager{pgDump: pgDump, pgRestore: pgRestore, createDB: createDB,
		maximumDatabaseBytes: maximumDatabaseBytes}, nil
}

func (manager *Manager) Check(ctx context.Context, databases []configspec.Database) error {
	if manager == nil || len(databases) == 0 {
		return errors.New("PostgreSQL backup manager is invalid")
	}
	for _, database := range databases {
		connection, err := connect(ctx, database)
		if err != nil {
			return fmt.Errorf("check PostgreSQL source %s: unavailable", database.Name)
		}
		var version string
		err = connection.QueryRow(ctx, serverVersionSQL).Scan(&version)
		closeErr := connection.Close(ctx)
		if err != nil || closeErr != nil || !supportedServerVersion(version) {
			return fmt.Errorf("check PostgreSQL source %s: incompatible", database.Name)
		}
	}
	return nil
}

func (manager *Manager) Backup(ctx context.Context, database configspec.Database, workDirectory string) (Snapshot, error) {
	startedAt := time.Now().UTC()
	connection, err := connect(ctx, database)
	if err != nil {
		return Snapshot{}, fmt.Errorf("connect PostgreSQL source %s: unavailable", database.Name)
	}
	defer connection.Close(ctx)
	transaction, err := connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin PostgreSQL snapshot %s: unavailable", database.Name)
	}
	defer transaction.Rollback(ctx)
	var snapshotID, serverVersion string
	if err := transaction.QueryRow(ctx, exportSnapshotSQL).Scan(&snapshotID); err != nil {
		return Snapshot{}, fmt.Errorf("export PostgreSQL snapshot %s: unavailable", database.Name)
	}
	if err := transaction.QueryRow(ctx, serverVersionSQL).Scan(&serverVersion); err != nil ||
		!supportedServerVersion(serverVersion) {
		return Snapshot{}, fmt.Errorf("read PostgreSQL server version %s: incompatible", database.Name)
	}
	schemaVersion, err := readSchemaVersion(ctx, transaction, database.SchemaKind, database.DeclaredSchemaVersion)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read PostgreSQL schema version %s: unavailable", database.Name)
	}
	directory := filepath.Join(workDirectory, database.Name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Snapshot{}, errors.New("create PostgreSQL backup workspace")
	}
	pgpass, err := writePGPass(directory, databaseCLIHost(database), database.Port, database.Database, database.User, database.Password)
	if err != nil {
		return Snapshot{}, err
	}
	defer os.Remove(pgpass)
	dumpPath := filepath.Join(directory, "database.dump")
	schemaPath := filepath.Join(directory, "schema.sql")
	common := []string{"--no-password", "--no-owner", "--no-privileges", "--snapshot=" + snapshotID}
	if database.Role != "" {
		common = append(common, "--role="+database.Role)
	}
	dumpArgs := append(append([]string{}, common...), "--format=custom", "--compress=zstd:6", "--file="+dumpPath)
	if err := manager.run(ctx, manager.pgDump, dumpArgs, databaseEnvironment(database, pgpass, database.Database)); err != nil {
		return Snapshot{}, fmt.Errorf("create PostgreSQL dump %s: failed", database.Name)
	}
	schemaArgs := append(append([]string{}, common...), "--schema-only", "--file="+schemaPath)
	if err := manager.run(ctx, manager.pgDump, schemaArgs, databaseEnvironment(database, pgpass, database.Database)); err != nil {
		return Snapshot{}, fmt.Errorf("create PostgreSQL schema dump %s: failed", database.Name)
	}
	if err := manager.run(ctx, manager.pgRestore, []string{"--list", dumpPath}, []string{"TZ=UTC"}); err != nil {
		return Snapshot{}, fmt.Errorf("validate PostgreSQL dump %s: failed", database.Name)
	}
	dumpDigest, dumpSize, err := digestFile(dumpPath, manager.maximumDatabaseBytes)
	if err != nil {
		return Snapshot{}, fmt.Errorf("digest PostgreSQL dump %s: failed", database.Name)
	}
	schemaDigest, schemaSize, err := digestFile(schemaPath, 64<<20)
	if err != nil {
		return Snapshot{}, fmt.Errorf("digest PostgreSQL schema %s: failed", database.Name)
	}
	return Snapshot{
		Name: database.Name, SchemaKind: database.SchemaKind, SchemaVersion: schemaVersion,
		ServerVersion: serverVersion, StartedAt: startedAt, FinishedAt: time.Now().UTC(),
		DumpPath: dumpPath, DumpDigest: dumpDigest, DumpSize: dumpSize,
		SchemaPath: schemaPath, SchemaDigest: schemaDigest, SchemaSize: schemaSize,
	}, nil
}

func (manager *Manager) Restore(ctx context.Context, target configspec.RestoreDatabase, source configspec.Database, dumpPath string, expectedSchemaVersion string) (RestoreReadback, error) {
	admin := databaseFromTarget(target, target.AdminDatabase)
	connection, err := connect(ctx, admin)
	if err != nil {
		return RestoreReadback{}, fmt.Errorf("connect restore administration database %s: unavailable", target.Name)
	}
	var exists bool
	err = connection.QueryRow(ctx, databaseExistsSQL, pgx.StrictNamedArgs{"database": target.Database}).Scan(&exists)
	closeErr := connection.Close(ctx)
	if err != nil || closeErr != nil {
		return RestoreReadback{}, fmt.Errorf("read restore target state %s: unavailable", target.Name)
	}
	if exists {
		return RestoreReadback{}, fmt.Errorf("restore target database %s already exists", target.Name)
	}
	directory := filepath.Dir(dumpPath)
	pgpass, err := writePGPass(directory, databaseCLIHost(admin), target.Port, "*", target.User, target.Password)
	if err != nil {
		return RestoreReadback{}, err
	}
	defer os.Remove(pgpass)
	if err := manager.run(ctx, manager.createDB,
		[]string{"--no-password", "--maintenance-db=" + target.AdminDatabase, "--template=template0", target.Database},
		databaseEnvironment(admin, pgpass, target.AdminDatabase)); err != nil {
		return RestoreReadback{}, fmt.Errorf("create isolated restore target %s: failed", target.Name)
	}
	restoreDatabase := databaseFromTarget(target, target.Database)
	if err := manager.run(ctx, manager.pgRestore,
		[]string{"--exit-on-error", "--single-transaction", "--no-owner", "--no-privileges", "--dbname=" + target.Database, dumpPath},
		databaseEnvironment(restoreDatabase, pgpass, target.Database)); err != nil {
		return RestoreReadback{}, fmt.Errorf("restore PostgreSQL database %s: failed", target.Name)
	}
	readbackConnection, err := connect(ctx, restoreDatabase)
	if err != nil {
		return RestoreReadback{}, fmt.Errorf("connect restored PostgreSQL database %s: unavailable", target.Name)
	}
	transaction, err := readbackConnection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		_ = readbackConnection.Close(ctx)
		return RestoreReadback{}, fmt.Errorf("begin restored PostgreSQL readback %s: unavailable", target.Name)
	}
	schemaVersion, schemaErr := readSchemaVersion(ctx, transaction, source.SchemaKind, source.DeclaredSchemaVersion)
	rollbackErr := transaction.Rollback(ctx)
	closeErr = readbackConnection.Close(ctx)
	if schemaErr != nil || rollbackErr != nil || closeErr != nil || schemaVersion != expectedSchemaVersion {
		return RestoreReadback{}, fmt.Errorf("restored PostgreSQL schema readback %s: mismatch", target.Name)
	}
	digest := sha256.Sum256([]byte(target.Name + "\x00" + target.Host + "\x00" + target.Database))
	return RestoreReadback{Name: target.Name, SchemaVersion: schemaVersion,
		TargetDigest: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func (manager *Manager) run(ctx context.Context, executable string, arguments, environment []string) error {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = environment
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("PostgreSQL tool execution failed")
	}
	return nil
}

func connect(ctx context.Context, database configspec.Database) (*pgx.Conn, error) {
	connectionURL := &url.URL{Scheme: "postgresql", User: url.User(database.User),
		Host: net.JoinHostPort(database.Host, strconv.Itoa(int(database.Port))), Path: database.Database}
	query := connectionURL.Query()
	query.Set("sslmode", "disable")
	connectionURL.RawQuery = query.Encode()
	config, err := pgx.ParseConfig(connectionURL.String())
	if err != nil {
		return nil, errors.New("build PostgreSQL connection configuration")
	}
	config.Password = database.Password
	config.RuntimeParams["application_name"] = "kodex-backup-controller"
	if database.TLSMode == "verify-full" {
		tlsConfig, err := loadTLS(database.TLSServerName, database.CAFile,
			database.ClientCertificateFile, database.ClientPrivateKeyFile)
		if err != nil {
			return nil, err
		}
		config.TLSConfig = tlsConfig
	}
	return pgx.ConnectConfig(ctx, config)
}

func loadTLS(serverName, caFile, certificateFile, keyFile string) (*tls.Config, error) {
	ca, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("read PostgreSQL CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse PostgreSQL CA")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName, RootCAs: roots}
	if certificateFile != "" {
		certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
		if err != nil {
			return nil, errors.New("load PostgreSQL client certificate")
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func readSchemaVersion(ctx context.Context, transaction pgx.Tx, kind, declared string) (string, error) {
	switch kind {
	case "goose":
		var version int64
		if err := transaction.QueryRow(ctx, gooseVersionSQL).Scan(&version); err != nil {
			return "", err
		}
		return "goose:" + strconv.FormatInt(version, 10), nil
	case "declared":
		return "declared:" + declared, nil
	default:
		return "", errors.New("unsupported schema version source")
	}
}

func databaseEnvironment(database configspec.Database, pgpass, selectedDatabase string) []string {
	environment := []string{
		"PGHOST=" + databaseCLIHost(database),
		"PGPORT=" + strconv.Itoa(int(database.Port)),
		"PGDATABASE=" + selectedDatabase,
		"PGUSER=" + database.User,
		"PGPASSFILE=" + pgpass,
		"PGSSLMODE=" + database.TLSMode,
		"TZ=UTC",
	}
	if database.TLSMode == "verify-full" {
		environment = append(environment, "PGSSLROOTCERT="+database.CAFile, "PGSSLSNI=1")
		if database.Host != database.TLSServerName {
			environment = append(environment, "PGHOSTADDR="+database.Host)
		}
		if database.ClientCertificateFile != "" {
			environment = append(environment, "PGSSLCERT="+database.ClientCertificateFile,
				"PGSSLKEY="+database.ClientPrivateKeyFile)
		}
	}
	return environment
}

func databaseCLIHost(database configspec.Database) string {
	if database.TLSMode == "verify-full" {
		return database.TLSServerName
	}
	return database.Host
}

func writePGPass(directory, host string, port uint16, database, user, password string) (string, error) {
	escape := func(value string) string {
		value = strings.ReplaceAll(value, `\`, `\\`)
		return strings.ReplaceAll(value, ":", `\:`)
	}
	path := filepath.Join(directory, ".pgpass")
	payload := strings.Join([]string{escape(host), strconv.Itoa(int(port)), escape(database), escape(user), escape(password)}, ":") + "\n"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		return "", errors.New("write protected PostgreSQL password file")
	}
	return path, nil
}

func digestFile(path string, maximumBytes int64) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, maximumBytes+1))
	if err != nil || size > maximumBytes {
		return "", 0, errors.New("file exceeds the configured boundary")
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}

func supportedServerVersion(value string) bool {
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(value) {
		return false
	}
	major, err := strconv.Atoi(value[:2])
	return err == nil && major >= 17 && major <= 18
}

func databaseFromTarget(target configspec.RestoreDatabase, database string) configspec.Database {
	return configspec.Database{
		Name: target.Name, Host: target.Host, Port: target.Port, Database: database,
		User: target.User, Password: target.Password, TLSMode: target.TLSMode,
		TLSServerName: target.TLSServerName, CAFile: target.CAFile,
		ClientCertificateFile: target.ClientCertificateFile,
		ClientPrivateKeyFile:  target.ClientPrivateKeyFile,
		SchemaKind:            "declared", DeclaredSchemaVersion: "restore",
	}
}
