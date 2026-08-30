package integrationfixture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	defaultListenAddress = ":8080"
	listenAddressEnv     = "KODEX_INTEGRATION_SYNTHETIC_LISTEN_ADDRESS"
)

func Run(lifecycleCtx, shutdownBaseCtx context.Context, output io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := NewHandler(NewStore())
	listenAddress, err := configuredListenAddress()
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	listener, err := (&net.ListenConfig{}).Listen(lifecycleCtx, "tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen for synthetic integration requests: %w", err)
	}
	handler.SetReady(true)
	logger.InfoContext(lifecycleCtx, "integration synthetic fixture started", "address", listener.Addr().String())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	var serveErr error
	select {
	case <-lifecycleCtx.Done():
	case serveErr = <-serveResult:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	}
	handler.SetReady(false)
	shutdownCtx, cancelShutdown := context.WithTimeout(shutdownBaseCtx, 5*time.Second)
	defer cancelShutdown()
	shutdownErr := server.Shutdown(shutdownCtx)
	if errors.Is(shutdownErr, http.ErrServerClosed) {
		shutdownErr = nil
	}
	logger.InfoContext(shutdownBaseCtx, "integration synthetic fixture stopped")
	return errors.Join(serveErr, shutdownErr)
}

func configuredListenAddress() (string, error) {
	address := os.Getenv(listenAddressEnv)
	if address == "" {
		return defaultListenAddress, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return "", errors.New("synthetic integration listen address is invalid")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || (portNumber != 0 && portNumber < 1024) || portNumber > 65535 {
		return "", errors.New("synthetic integration listen port is invalid")
	}
	return address, nil
}
