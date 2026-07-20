package testsupport

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const generatedPostgresOwner = "mattercodex_test_owner"

type GeneratedPostgresHarness struct {
	BootstrapDSN         string
	BootstrapProof       string
	LoopbackBootstrapDSN string
	MajorVersion         string

	rootDirectory   string
	dataDirectory   string
	socketDirectory string
	binDirectory    string
	serverPort      int
}

func StartGeneratedPostgresHarness(ctx context.Context) (GeneratedPostgresHarness, error) {
	binDirectory, err := generatedPostgresBinDirectory(ctx)
	if err != nil {
		return GeneratedPostgresHarness{}, err
	}
	rootDirectory, err := os.MkdirTemp("", "mattercodex-postgres-harness-")
	if err != nil {
		return GeneratedPostgresHarness{}, fmt.Errorf("временный каталог generated PostgreSQL не создан")
	}
	harness := GeneratedPostgresHarness{
		rootDirectory:   rootDirectory,
		dataDirectory:   filepath.Join(rootDirectory, "data"),
		socketDirectory: filepath.Join(rootDirectory, "socket"),
		binDirectory:    binDirectory,
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = harness.Close(context.WithoutCancel(ctx))
		}
	}()
	if err := os.Mkdir(harness.socketDirectory, 0o700); err != nil {
		return GeneratedPostgresHarness{}, fmt.Errorf("socket-каталог generated PostgreSQL не создан")
	}
	harness.serverPort, err = reserveGeneratedPostgresPort()
	if err != nil {
		return GeneratedPostgresHarness{}, err
	}
	initContext, cancelInit := context.WithTimeout(ctx, 30*time.Second)
	defer cancelInit()
	initCommand := exec.CommandContext(initContext, filepath.Join(binDirectory, "initdb"),
		"--pgdata", harness.dataDirectory,
		"--username", generatedPostgresOwner,
		"--auth-local", "trust",
		"--auth-host", "trust",
		"--encoding", "UTF8",
		"--no-locale",
	)
	if err := initCommand.Run(); err != nil {
		return GeneratedPostgresHarness{}, fmt.Errorf("initdb generated PostgreSQL не выполнен")
	}
	registryContext, cancelRegistry := context.WithTimeout(ctx, 15*time.Second)
	defer cancelRegistry()
	if err := initializeGeneratedProofRegistry(registryContext, binDirectory, harness.dataDirectory); err != nil {
		return GeneratedPostgresHarness{}, err
	}
	startContext, cancelStart := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStart()
	startOptions := strings.Join([]string{
		"-F",
		"-k", harness.socketDirectory,
		"-p", strconv.Itoa(harness.serverPort),
		"-c", "listen_addresses=127.0.0.1",
		"-c", "unix_socket_permissions=0700",
	}, " ")
	startCommand := exec.CommandContext(startContext, filepath.Join(binDirectory, "pg_ctl"),
		"--pgdata", harness.dataDirectory,
		"--wait", "--timeout", "30",
		"--options", startOptions,
		"start",
	)
	if err := startCommand.Run(); err != nil {
		return GeneratedPostgresHarness{}, fmt.Errorf("generated PostgreSQL server не запущен")
	}
	harness.BootstrapDSN = fmt.Sprintf(
		"host=%s port=%d user=%s dbname=postgres connect_timeout=5",
		harness.socketDirectory, harness.serverPort, generatedPostgresOwner,
	)
	harness.LoopbackBootstrapDSN = fmt.Sprintf(
		"host=127.0.0.1 port=%d user=%s dbname=postgres connect_timeout=5",
		harness.serverPort, generatedPostgresOwner,
	)
	proofContext, cancelProof := context.WithTimeout(ctx, 15*time.Second)
	defer cancelProof()
	harness.BootstrapProof, err = provisionGeneratedBootstrapProof(
		proofContext,
		harness.BootstrapDSN,
		harness.dataDirectory,
		harness.socketDirectory,
	)
	if err != nil {
		return GeneratedPostgresHarness{}, err
	}
	versionOutput, err := exec.CommandContext(ctx, filepath.Join(binDirectory, "postgres"), "--version").Output()
	if err == nil {
		fields := strings.Fields(string(versionOutput))
		if len(fields) > 2 {
			harness.MajorVersion = strings.SplitN(fields[2], ".", 2)[0]
		}
	}
	cleanup = false
	return harness, nil
}

func reserveGeneratedPostgresPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("generated PostgreSQL harness не зарезервировал loopback port")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("generated PostgreSQL harness не освободил loopback port")
	}
	return port, nil
}

// ServerBinDirectory возвращает каталог server binaries только для вложенных required-тестов.
func (harness GeneratedPostgresHarness) ServerBinDirectory() string {
	return harness.binDirectory
}

func initializeGeneratedProofRegistry(ctx context.Context, binDirectory string, dataDirectory string) error {
	table := bootstrapProofTable
	statement := fmt.Sprintf(`
create table public.%s (
	nonce_sha256 bytea primary key,
	version integer not null,
	issued_at timestamptz not null,
	expires_at timestamptz not null,
	endpoint_fingerprint text not null,
	server_fingerprint text not null,
	maintenance_database text not null,
		purpose text not null,
		run_id text not null unique,
		consumed_at timestamptz,
		consumed_by text,
		target_database text unique,
		target_marker_sha256 bytea,
		target_owner_oid oid,
		target_database_oid oid,
		target_state text,
		target_reserved_at timestamptz,
		target_identified_at timestamptz,
		target_marker_applied_at timestamptz,
		target_dropped_at timestamptz,
		constraint mattercodex_test_bootstrap_proofs_nonce_check check (octet_length(nonce_sha256) = 32),
		constraint mattercodex_test_bootstrap_proofs_marker_check check (target_marker_sha256 is null or octet_length(target_marker_sha256) = 32),
		constraint mattercodex_test_bootstrap_proofs_lifetime_check check (expires_at > issued_at and expires_at <= issued_at + interval '10 minutes'),
		constraint mattercodex_test_bootstrap_proofs_state_check check (
			(consumed_at is null and consumed_by is null and target_database is null and target_marker_sha256 is null and
				target_owner_oid is null and target_database_oid is null and target_state is null and target_reserved_at is null and
				target_identified_at is null and target_marker_applied_at is null and target_dropped_at is null)
			or
			(consumed_at is not null and consumed_by is not null and target_database is not null and target_marker_sha256 is not null and
				target_owner_oid is not null and target_state in ('reserved', 'created', 'marked', 'dropped') and target_reserved_at is not null and
				(target_database_oid is not null or target_state in ('reserved', 'dropped')) and
				(target_identified_at is not null or target_database_oid is null) and
				(target_marker_applied_at is not null or target_state in ('reserved', 'created', 'dropped')) and
				(target_dropped_at is not null or target_state <> 'dropped'))
		)
	);
revoke all on table public.%s from public;
create function public.%s() returns trigger
language plpgsql
set search_path = pg_catalog, public
as $guard$
begin
	if row(new.nonce_sha256, new.version, new.issued_at, new.expires_at, new.endpoint_fingerprint,
		new.server_fingerprint, new.maintenance_database, new.purpose, new.run_id)
		is distinct from
		row(old.nonce_sha256, old.version, old.issued_at, old.expires_at, old.endpoint_fingerprint,
		old.server_fingerprint, old.maintenance_database, old.purpose, old.run_id) then
		raise exception 'MC_TEST_BOOTSTRAP_IMMUTABLE_PROOF';
	end if;
	if old.consumed_at is null then
		if new.consumed_at is null or new.consumed_by is null or new.target_database is null or
			new.target_marker_sha256 is null or new.target_owner_oid is null or new.target_database_oid is not null or
			new.target_state <> 'reserved' or new.target_reserved_at is null or new.target_identified_at is not null or
			new.target_marker_applied_at is not null or new.target_dropped_at is not null then
			raise exception 'MC_TEST_BOOTSTRAP_INVALID_RESERVATION';
		end if;
		return new;
	end if;
	if row(new.consumed_at, new.consumed_by, new.target_database, new.target_marker_sha256,
		new.target_owner_oid, new.target_reserved_at)
		is distinct from
		row(old.consumed_at, old.consumed_by, old.target_database, old.target_marker_sha256,
		old.target_owner_oid, old.target_reserved_at) then
		raise exception 'MC_TEST_BOOTSTRAP_IMMUTABLE_RESERVATION';
	end if;
	if old.target_database_oid is not null and new.target_database_oid is distinct from old.target_database_oid then
		raise exception 'MC_TEST_BOOTSTRAP_IMMUTABLE_DATABASE_OID';
	end if;
	if old.target_identified_at is not null and new.target_identified_at is distinct from old.target_identified_at then
		raise exception 'MC_TEST_BOOTSTRAP_IMMUTABLE_IDENTIFIED_AT';
	end if;
	if old.target_marker_applied_at is not null and new.target_marker_applied_at is distinct from old.target_marker_applied_at then
		raise exception 'MC_TEST_BOOTSTRAP_IMMUTABLE_MARKER_AT';
	end if;
	if old.target_dropped_at is not null and new.target_dropped_at is distinct from old.target_dropped_at then
		raise exception 'MC_TEST_BOOTSTRAP_IMMUTABLE_DROPPED_AT';
	end if;
	if old.target_state = 'reserved' and new.target_state = 'created' then
		if new.target_database_oid is null or new.target_identified_at is null or
			new.target_marker_applied_at is not null or new.target_dropped_at is not null then
			raise exception 'MC_TEST_BOOTSTRAP_INVALID_CREATED_TRANSITION';
		end if;
		return new;
	end if;
	if old.target_state = 'created' and new.target_state = 'marked' then
		if new.target_database_oid is null or new.target_identified_at is null or
			new.target_marker_applied_at is null or new.target_dropped_at is not null then
			raise exception 'MC_TEST_BOOTSTRAP_INVALID_MARKED_TRANSITION';
		end if;
		return new;
	end if;
	if old.target_state in ('reserved', 'created', 'marked') and new.target_state = 'dropped' then
		if new.target_dropped_at is null then
			raise exception 'MC_TEST_BOOTSTRAP_INVALID_DROPPED_TRANSITION';
		end if;
		return new;
	end if;
	if new is not distinct from old then
		return new;
	end if;
	raise exception 'MC_TEST_BOOTSTRAP_INVALID_STATE_TRANSITION';
end;
$guard$;
revoke all on function public.%s() from public;
create trigger %s
before update on public.%s
for each row execute function public.%s();
`, table, table, bootstrapProofGuardFunction, bootstrapProofGuardFunction,
		bootstrapProofGuardTrigger, table, bootstrapProofGuardFunction)
	command := exec.CommandContext(ctx, filepath.Join(binDirectory, "postgres"),
		"--single", "-D", dataDirectory, "postgres",
	)
	command.Env = append(os.Environ(), "LC_ALL=C")
	command.Stdin = strings.NewReader(strings.Join(strings.Fields(statement), " ") + "\n")
	if err := command.Run(); err != nil {
		return fmt.Errorf("generated PostgreSQL bootstrap не подготовил proof registry в принадлежащем PGDATA")
	}
	return nil
}

func (harness *GeneratedPostgresHarness) Close(ctx context.Context) error {
	if harness == nil || strings.TrimSpace(harness.rootDirectory) == "" {
		return nil
	}
	stopContext, cancelStop := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStop()
	stopErr := exec.CommandContext(stopContext, filepath.Join(harness.binDirectory, "pg_ctl"),
		"--pgdata", harness.dataDirectory,
		"--wait", "--timeout", "30",
		"--mode", "fast",
		"stop",
	).Run()
	removeErr := os.RemoveAll(harness.rootDirectory)
	harness.rootDirectory = ""
	if stopErr != nil {
		return fmt.Errorf("generated PostgreSQL server не остановлен")
	}
	if removeErr != nil {
		return fmt.Errorf("временный каталог generated PostgreSQL не удалён")
	}
	return nil
}

func generatedPostgresBinDirectory(ctx context.Context) (string, error) {
	candidates := make([]string, 0, 8)
	if configured := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_BINDIR")); configured != "" {
		candidates = append(candidates, configured)
	}
	if configuredMajor := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_MAJOR")); configuredMajor != "" {
		candidates = append(candidates, filepath.Join("/usr/lib/postgresql", configuredMajor, "bin"))
	}
	if output, err := exec.CommandContext(ctx, "pg_config", "--bindir").Output(); err == nil {
		candidates = append(candidates, strings.TrimSpace(string(output)))
	}
	installed, _ := filepath.Glob("/usr/lib/postgresql/*/bin")
	sort.Sort(sort.Reverse(sort.StringSlice(installed)))
	candidates = append(candidates, installed...)
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if candidate == "." || seen[candidate] {
			continue
		}
		seen[candidate] = true
		valid := true
		for _, binary := range []string{"initdb", "pg_ctl", "postgres"} {
			info, err := os.Stat(filepath.Join(candidate, binary))
			if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
				valid = false
				break
			}
		}
		if valid {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("generated PostgreSQL harness не нашёл server binaries; задайте MATTERCODEX_POSTGRES_TEST_BINDIR")
}
