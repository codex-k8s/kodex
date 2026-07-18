package main

import (
	"context"
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
	var target testsupport.DisposableDatabase
	created := false
	if targetDSN != "" || targetMarker != "" {
		if targetDSN == "" || targetMarker == "" || testsupport.ValidateDisposableDatabase(ctx, targetDSN, targetMarker) != nil {
			fmt.Fprintln(os.Stderr, "заданный PostgreSQL test target не прошёл fail-closed admission")
			return 1
		}
	} else {
		if bootstrapDSN == "" {
			fmt.Fprintln(os.Stderr, "нужен локальный bootstrap DSN либо заранее созданный одноразовый PostgreSQL target")
			return 1
		}
		var err error
		target, err = testsupport.BootstrapDisposableDatabase(ctx, bootstrapDSN)
		if err != nil {
			fmt.Fprintf(os.Stderr, "создание одноразового PostgreSQL test target не выполнено: %v\n", err)
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
	runErr := command.Run()
	cleanupErr := error(nil)
	if created {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr = testsupport.DestroyDisposableDatabase(cleanupCtx, bootstrapDSN, target)
		cleanupCancel()
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
