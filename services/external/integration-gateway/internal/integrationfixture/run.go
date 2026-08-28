package integrationfixture

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const listenAddress = ":8080"

func Run(lifecycleCtx, shutdownBaseCtx context.Context, output io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := NewHandler(NewStore())
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
		return errors.New("listen for synthetic integration requests")
	}
	handler.SetReady(true)
	logger.InfoContext(lifecycleCtx, "integration synthetic fixture started")
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
