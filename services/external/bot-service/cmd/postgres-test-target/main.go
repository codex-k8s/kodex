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
	if len(arguments) > 0 && arguments[0] == "--" {
		arguments = arguments[1:]
	}
	if len(arguments) == 0 {
		fmt.Fprintln(os.Stderr, "не задана команда для PostgreSQL test target")
		return 2
	}
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
	} else {
		if bootstrapDSN == "" {
			var err error
			harness, err = testsupport.StartGeneratedPostgresHarness(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "generated PostgreSQL test harness не запущен")
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
	command.Env = append(os.Environ(),
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN="+targetDSN,
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER="+targetMarker,
		"MATTERCODEX_POSTGRES_TEST_REQUIRED=1",
	)
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
