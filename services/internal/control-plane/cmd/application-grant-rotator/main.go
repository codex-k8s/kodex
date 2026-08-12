package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/readinessgrant"
)

const (
	signerDirectory = "/var/run/secrets/mattercodex/control-plane/readiness-grants"
	namespaceFile   = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	tokenFile       = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caFile          = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(os.Stderr, "application grant rotator failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	namespaceRaw, err := os.ReadFile(namespaceFile)
	if err != nil || len(namespaceRaw) == 0 {
		return errors.New("read workload namespace")
	}
	patcher, err := readinessgrant.NewKubernetesPatcher(
		"https://kubernetes.default.svc:443", "kubernetes.default.svc", caFile, tokenFile, 10*time.Second,
	)
	if err != nil {
		return err
	}
	rotator, err := readinessgrant.New(
		strings.TrimSpace(string(namespaceRaw)), 4*time.Minute, time.Minute,
		readinessgrant.DefaultTargets(signerDirectory), patcher,
	)
	if err != nil {
		return err
	}
	technical := &http.Server{Addr: ":9090", ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 30 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		if !rotator.Ready() {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	technical.Handler = mux
	errCh := make(chan error, 2)
	go func() { errCh <- rotator.Run(ctx) }()
	go func() {
		if serveErr := technical.ListenAndServe(); !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return technical.Shutdown(shutdownCtx)
	case runErr := <-errCh:
		return runErr
	}
}
