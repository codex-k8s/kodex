package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
)

func main() {
	os.Exit(run())
}

func run() int {
	arguments := os.Args[1:]
	majors := []string{""}
	if len(arguments) >= 2 && arguments[0] == "--majors" {
		var err error
		majors, err = parsePostgresMajors(arguments[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "PostgreSQL test matrix имеет недопустимый список версий")
			return 2
		}
		arguments = arguments[2:]
	}
	if len(arguments) > 0 && arguments[0] == "--" {
		arguments = arguments[1:]
	}
	if len(arguments) == 0 {
		fmt.Fprintln(os.Stderr, "не задана команда для PostgreSQL test target")
		return 2
	}
	if len(majors) == 1 && majors[0] == "" {
		return runTarget(arguments, "")
	}
	result := 0
	for _, major := range majors {
		if runMajorTarget(arguments, major) != 0 {
			result = 1
		}
	}
	return result
}

func runMajorTarget(arguments []string, major string) int {
	restore, err := selectMajorEnvironment(major)
	if err != nil {
		fmt.Fprintf(os.Stderr, "PostgreSQL %s test suite: NOT RUN (неполная безопасная конфигурация disposable target)\n", major)
		return 1
	}
	defer restore()
	fmt.Printf("PostgreSQL %s test suite: RUNNING\n", major)
	result := runTarget(arguments, major)
	switch result {
	case 0:
		fmt.Printf("PostgreSQL %s test suite: PASS\n", major)
		return 0
	case -1:
		mode, _ := parsePostgresTestMode(os.Getenv("MATTERCODEX_TEST_POSTGRES_MODE"))
		fmt.Fprintf(os.Stderr, "PostgreSQL %s test suite: NOT RUN (%s disposable target недоступен)\n", major, mode)
	default:
		fmt.Fprintf(os.Stderr, "PostgreSQL %s test suite: FAIL\n", major)
		return 1
	}
	return 1
}

func runTarget(arguments []string, expectedMajor string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	mode, err := parsePostgresTestMode(os.Getenv("MATTERCODEX_TEST_POSTGRES_MODE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL test mode не прошёл fail-closed admission")
		return 1
	}
	sentinelDirectory, sentinelPath, sentinelValue, err := newPostgresExecutionSentinel(expectedMajor)
	if err != nil {
		fmt.Fprintln(os.Stderr, "обязательный PostgreSQL sentinel не создан")
		return 1
	}
	defer os.RemoveAll(sentinelDirectory)
	targetDSN := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"))
	targetMarker := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER"))
	bootstrapDSN := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN"))
	bootstrapProof := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF"))
	var target testsupport.DisposableDatabase
	var harness testsupport.GeneratedPostgresHarness
	var controller *controllerPostgresHarness
	created := false
	generated := false
	if targetDSN != "" || targetMarker != "" {
		if mode != postgresTestModeScopedDSN {
			fmt.Fprintln(os.Stderr, "внешний PostgreSQL target разрешён только в scoped-dsn mode")
			return 1
		}
		if targetDSN == "" || targetMarker == "" || testsupport.ValidateDisposableDatabase(ctx, targetDSN, targetMarker) != nil {
			fmt.Fprintln(os.Stderr, "заданный PostgreSQL test target не прошёл fail-closed admission")
			return 1
		}
		if expectedMajor != "" && testsupport.ValidatePostgresMajor(ctx, targetDSN, expectedMajor) != nil {
			fmt.Fprintf(os.Stderr, "заданный PostgreSQL test target не является PostgreSQL %s\n", expectedMajor)
			return 1
		}
	} else {
		if bootstrapDSN != "" && mode != postgresTestModeScopedDSN {
			fmt.Fprintln(os.Stderr, "внешний PostgreSQL bootstrap разрешён только в scoped-dsn mode")
			return 1
		}
		if bootstrapDSN == "" && mode == postgresTestModeScopedDSN {
			fmt.Fprintln(os.Stderr, "scoped-dsn mode не получил полную disposable конфигурацию")
			return 1
		}
		if bootstrapDSN == "" && mode == postgresTestModeLocalBinaries {
			var err error
			harness, err = testsupport.StartGeneratedPostgresHarness(ctx)
			if err != nil {
				if expectedMajor == "" {
					fmt.Fprintln(os.Stderr, "generated PostgreSQL test harness: NOT RUN")
					return 1
				}
				return -1
			}
			if expectedMajor != "" && harness.MajorVersion != expectedMajor {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = harness.Close(cleanupCtx)
				cleanupCancel()
				fmt.Fprintf(os.Stderr, "PostgreSQL %s generated test harness получил несовпадающую версию\n", expectedMajor)
				return 1
			}
			generated = true
			bootstrapDSN = harness.BootstrapDSN
			bootstrapProof = harness.BootstrapProof
		}
		if bootstrapDSN == "" && (mode == postgresTestModeDocker || mode == postgresTestModeKubernetes) {
			var err error
			controller, err = startControllerPostgres(ctx, mode, expectedMajor)
			if err != nil {
				return -1
			}
			bootstrapDSN = controller.bootstrapDSN
			bootstrapProof = controller.bootstrapProof
		}
		var err error
		target, err = testsupport.BootstrapDisposableDatabase(ctx, bootstrapDSN, bootstrapProof)
		if err != nil {
			if generated {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = harness.Close(cleanupCtx)
				cleanupCancel()
			}
			if controller != nil {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = controller.Close(cleanupCtx)
				cleanupCancel()
			}
			if !generated && target.Database != "" {
				fmt.Fprintf(
					os.Stderr,
					"PostgreSQL test target %s сохранён или зарезервирован; состояние и очистку должен сверить владелец ephemeral endpoint/controller\n",
					target.Database,
				)
			} else {
				fmt.Fprintln(os.Stderr, "создание одноразового PostgreSQL test target не выполнено")
			}
			return 1
		}
		created = true
		targetDSN = target.DSN
		targetMarker = target.Marker
	}
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	goCacheEnvironment, err := materializeGoCacheEnvironment(ctx, os.Environ())
	if err != nil {
		fmt.Fprintln(os.Stderr, "безопасные Go cache paths не материализованы")
		return 1
	}
	command.Env = append(postgresTestCommandEnvironment(os.Environ()),
		goCacheEnvironment...)
	command.Env = append(command.Env, safeOfflineGoEnvironment(os.Environ())...)
	command.Env = append(command.Env,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN="+targetDSN,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER="+targetMarker,
		"MATTERCODEX_POSTGRES_TEST_REQUIRED=1",
		"MATTERCODEX_POSTGRES_SENTINEL_PATH="+sentinelPath,
		"MATTERCODEX_POSTGRES_SENTINEL_VALUE="+sentinelValue,
		"HOME="+sentinelDirectory,
		"GOENV=off",
		"GOFLAGS=",
		"GOWORK=off",
	)
	if expectedMajor != "" {
		command.Env = append(command.Env, "MATTERCODEX_POSTGRES_TEST_MAJOR="+expectedMajor)
	}
	if generated {
		command.Env = append(command.Env, "MATTERCODEX_POSTGRES_TEST_BINDIR="+harness.ServerBinDirectory())
	}
	runErr := command.Run()
	if sentinelErr := verifyPostgresExecutionSentinel(sentinelPath, expectedMajor, sentinelValue); sentinelErr != nil {
		fmt.Fprintln(os.Stderr, "обязательный PostgreSQL sentinel не подтвердил фактическое выполнение test suite")
		if runErr == nil {
			runErr = sentinelErr
		}
	}
	cleanupErr := error(nil)
	if created {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr = testsupport.DestroyDisposableDatabase(cleanupCtx, bootstrapDSN, target)
		cleanupCancel()
		var handoffError testsupport.ExternalCleanupRequiredError
		if errors.As(cleanupErr, &handoffError) {
			fmt.Fprintf(
				os.Stderr,
				"PostgreSQL test target сохранён; очистку database %s должен выполнить владелец ephemeral endpoint/controller\n",
				handoffError.Database,
			)
			cleanupErr = nil
		}
	}
	if generated {
		harnessContext, harnessCancel := context.WithTimeout(context.Background(), 30*time.Second)
		harnessErr := harness.Close(harnessContext)
		harnessCancel()
		if cleanupErr == nil {
			cleanupErr = harnessErr
		}
	}
	if controller != nil {
		controllerContext, controllerCancel := context.WithTimeout(context.Background(), 30*time.Second)
		controllerErr := controller.Close(controllerContext)
		controllerCancel()
		if cleanupErr == nil {
			cleanupErr = controllerErr
		}
	}
	if cleanupErr != nil {
		fmt.Fprintln(os.Stderr, "безопасное удаление одноразового PostgreSQL test target не выполнено")
		return 1
	}
	if runErr == nil {
		return 0
	}
	if exitError, ok := runErr.(*exec.ExitError); ok {
		return exitError.ExitCode()
	}
	fmt.Fprintln(os.Stderr, "команда PostgreSQL test target не запущена")
	return 1
}

type postgresTestMode string

const (
	postgresTestModeKubernetes    postgresTestMode = "kubernetes"
	postgresTestModeDocker        postgresTestMode = "docker"
	postgresTestModeLocalBinaries postgresTestMode = "local-binaries"
	postgresTestModeScopedDSN     postgresTestMode = "scoped-dsn"
)

func parsePostgresTestMode(raw string) (postgresTestMode, error) {
	mode := postgresTestMode(strings.ToLower(strings.TrimSpace(raw)))
	if mode == "" {
		mode = postgresTestModeLocalBinaries
	}
	switch mode {
	case postgresTestModeKubernetes, postgresTestModeDocker, postgresTestModeLocalBinaries, postgresTestModeScopedDSN:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported PostgreSQL test mode")
	}
}

func newPostgresExecutionSentinel(major string) (string, string, string, error) {
	if major != "15" && major != "16" {
		return "", "", "", fmt.Errorf("PostgreSQL sentinel требует exact major")
	}
	directory, err := os.MkdirTemp("", "mattercodex-postgres-sentinel-")
	if err != nil {
		return "", "", "", err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.RemoveAll(directory)
		return "", "", "", err
	}
	valueBytes := make([]byte, 24)
	if _, err := rand.Read(valueBytes); err != nil {
		_ = os.RemoveAll(directory)
		return "", "", "", err
	}
	return directory, filepath.Join(directory, "executed"), hex.EncodeToString(valueBytes), nil
}

func verifyPostgresExecutionSentinel(path string, major string, value string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(body) != major+"\n"+value+"\n" {
		return fmt.Errorf("PostgreSQL sentinel identity не совпала")
	}
	return nil
}

func parsePostgresMajors(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	majors := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		major := strings.TrimSpace(part)
		if major != "15" && major != "16" {
			return nil, fmt.Errorf("unsupported PostgreSQL major")
		}
		if !seen[major] {
			seen[major] = true
			majors = append(majors, major)
		}
	}
	if len(majors) == 0 {
		return nil, fmt.Errorf("empty PostgreSQL matrix")
	}
	return majors, nil
}

func selectMajorEnvironment(major string) (func(), error) {
	names := []string{
		"MATTERCODEX_POSTGRES_TEST_MAJOR",
		"MATTERCODEX_POSTGRES_TEST_BINDIR",
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN",
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER",
		"MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN",
		"MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF",
	}
	type previousValue struct {
		value string
		set   bool
	}
	previous := make(map[string]previousValue, len(names))
	for _, name := range names {
		value, set := os.LookupEnv(name)
		previous[name] = previousValue{value: value, set: set}
	}
	restore := func() {
		for _, name := range names {
			state := previous[name]
			if state.set {
				_ = os.Setenv(name, state.value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
	}
	if err := os.Setenv("MATTERCODEX_POSTGRES_TEST_MAJOR", major); err != nil {
		restore()
		return func() {}, err
	}
	if scopedBinDirectory := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_BINDIR_" + major)); scopedBinDirectory != "" {
		if err := os.Setenv("MATTERCODEX_POSTGRES_TEST_BINDIR", scopedBinDirectory); err != nil {
			restore()
			return func() {}, err
		}
	} else {
		_ = os.Unsetenv("MATTERCODEX_POSTGRES_TEST_BINDIR")
	}
	pairs := [][3]string{
		{"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN", "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER", "target"},
		{"MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN", "MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF", "bootstrap"},
	}
	for _, pair := range pairs {
		first := strings.TrimSpace(os.Getenv(pair[0] + "_" + major))
		second := strings.TrimSpace(os.Getenv(pair[1] + "_" + major))
		if (first == "") != (second == "") {
			restore()
			return func() {}, fmt.Errorf("incomplete %s configuration", pair[2])
		}
		if first == "" {
			_ = os.Unsetenv(pair[0])
			_ = os.Unsetenv(pair[1])
			continue
		}
		if err := os.Setenv(pair[0], first); err != nil {
			restore()
			return func() {}, err
		}
		if err := os.Setenv(pair[1], second); err != nil {
			restore()
			return func() {}, err
		}
	}
	return restore, nil
}

func postgresTestCommandEnvironment(source []string) []string {
	allowedNames := map[string]bool{
		"PATH": true, "TMPDIR": true,
		"GOROOT":      true,
		"CGO_ENABLED": true, "CC": true, "CXX": true, "AR": true, "PKG_CONFIG_PATH": true,
		"LANG": true, "LC_ALL": true, "TZ": true,
	}
	result := make([]string, 0, len(source))
	for _, item := range source {
		name, _, ok := strings.Cut(item, "=")
		if ok && allowedNames[name] {
			result = append(result, item)
		}
	}
	return result
}

func materializeGoCacheEnvironment(ctx context.Context, source []string) ([]string, error) {
	allowedNames := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "GOROOT": true}
	discoveryEnvironment := make([]string, 0, len(source)+3)
	for _, item := range source {
		name, _, ok := strings.Cut(item, "=")
		if ok && allowedNames[name] {
			discoveryEnvironment = append(discoveryEnvironment, item)
		}
	}
	discoveryEnvironment = append(discoveryEnvironment, "GOENV=off", "GOFLAGS=", "GOWORK=off")
	command := exec.CommandContext(ctx, "go", "env", "-json", "GOMODCACHE", "GOCACHE", "GOPATH")
	command.Env = discoveryEnvironment
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go cache discovery failed")
	}
	values := map[string]string{}
	if err := json.Unmarshal(output, &values); err != nil {
		return nil, fmt.Errorf("go cache discovery output invalid")
	}
	result := make([]string, 0, 3)
	for _, name := range []string{"GOMODCACHE", "GOCACHE", "GOPATH"} {
		value := strings.TrimSpace(values[name])
		if value == "" {
			return nil, fmt.Errorf("go cache path missing")
		}
		for _, path := range filepath.SplitList(value) {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return nil, fmt.Errorf("go cache path is not absolute")
			}
		}
		result = append(result, name+"="+value)
	}
	return result, nil
}

func safeOfflineGoEnvironment(source []string) []string {
	values := map[string]string{}
	for _, item := range source {
		name, value, ok := strings.Cut(item, "=")
		if ok {
			values[name] = strings.TrimSpace(value)
		}
	}
	result := []string{}
	if values["GOPROXY"] == "off" {
		result = append(result, "GOPROXY=off")
	}
	if values["GOSUMDB"] == "off" {
		result = append(result, "GOSUMDB=off")
	}
	return result
}
