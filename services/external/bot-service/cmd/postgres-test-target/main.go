package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
		fmt.Fprintf(os.Stderr, "PostgreSQL %s test suite: NOT RUN (generated disposable target недоступен)\n", major)
	default:
		fmt.Fprintf(os.Stderr, "PostgreSQL %s test suite: FAIL\n", major)
		return 1
	}
	return 1
}

func runTarget(arguments []string, expectedMajor string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	targetDSN := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"))
	targetMarker := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER"))
	bootstrapDSN := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN"))
	bootstrapProof := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF"))
	var target testsupport.DisposableDatabase
	var harness testsupport.GeneratedPostgresHarness
	created := false
	generated := false
	if targetDSN != "" || targetMarker != "" {
		if targetDSN == "" || targetMarker == "" || testsupport.ValidateDisposableDatabase(ctx, targetDSN, targetMarker) != nil {
			fmt.Fprintln(os.Stderr, "заданный PostgreSQL test target не прошёл fail-closed admission")
			return 1
		}
		if expectedMajor != "" && testsupport.ValidatePostgresMajor(ctx, targetDSN, expectedMajor) != nil {
			fmt.Fprintf(os.Stderr, "заданный PostgreSQL test target не является PostgreSQL %s\n", expectedMajor)
			return 1
		}
	} else {
		if bootstrapDSN == "" {
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
		var err error
		target, err = testsupport.BootstrapDisposableDatabase(ctx, bootstrapDSN, bootstrapProof)
		if err != nil {
			if generated {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = harness.Close(cleanupCtx)
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
	command.Env = append(postgresTestCommandEnvironment(os.Environ()),
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN="+targetDSN,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER="+targetMarker,
		"MATTERCODEX_POSTGRES_TEST_REQUIRED=1",
	)
	if expectedMajor != "" {
		command.Env = append(command.Env, "MATTERCODEX_POSTGRES_TEST_MAJOR="+expectedMajor)
	}
	if generated {
		command.Env = append(command.Env, "MATTERCODEX_POSTGRES_TEST_BINDIR="+harness.ServerBinDirectory())
	}
	runErr := command.Run()
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
	sensitiveNames := map[string]bool{
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN":       true,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER":    true,
		"MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN":      true,
		"MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF":       true,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_15":    true,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER_15": true,
		"MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN_15":   true,
		"MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF_15":    true,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_16":    true,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER_16": true,
		"MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN_16":   true,
		"MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF_16":    true,
		"MATTERCODEX_POSTGRES_TEST_BINDIR_15":             true,
		"MATTERCODEX_POSTGRES_TEST_BINDIR_16":             true,
	}
	result := make([]string, 0, len(source))
	for _, item := range source {
		name, _, ok := strings.Cut(item, "=")
		if ok && sensitiveNames[name] {
			continue
		}
		result = append(result, item)
	}
	return result
}
