package testsupport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const generatedPostgresOwner = "mattercodex_test_owner"

type GeneratedPostgresHarness struct {
	BootstrapDSN   string
	BootstrapProof string
	MajorVersion   string

	rootDirectory   string
	dataDirectory   string
	socketDirectory string
	binDirectory    string
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
	initContext, cancelInit := context.WithTimeout(ctx, 30*time.Second)
	defer cancelInit()
	initCommand := exec.CommandContext(initContext, filepath.Join(binDirectory, "initdb"),
		"--pgdata", harness.dataDirectory,
		"--username", generatedPostgresOwner,
		"--auth-local", "trust",
		"--auth-host", "reject",
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
		"-p", "55432",
		"-c", "listen_addresses=",
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
		"host=%s port=55432 user=%s dbname=postgres connect_timeout=5",
		harness.socketDirectory, generatedPostgresOwner,
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
	constraint mattercodex_test_bootstrap_proofs_nonce_check check (octet_length(nonce_sha256) = 32),
	constraint mattercodex_test_bootstrap_proofs_lifetime_check check (expires_at > issued_at and expires_at <= issued_at + interval '10 minutes'),
	constraint mattercodex_test_bootstrap_proofs_state_check check ((consumed_at is null) = (consumed_by is null))
);
revoke all on table public.%s from public;
`, table, table)
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
