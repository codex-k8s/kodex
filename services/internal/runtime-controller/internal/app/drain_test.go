package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestCallbackListenerSurvivesLifecycleUntilRuntimeReadback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	runtimeDone := make(chan struct{})
	finished := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		finished <- runCallbackDuringDrain(ctx, runtimeDone, time.Second, func(callbackContext context.Context) error {
			server := &http.Server{BaseContext: func(net.Listener) context.Context { return callbackContext }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Context().Err() != nil {
					w.WriteHeader(503)
					return
				}
				w.WriteHeader(204)
			})}
			served := make(chan error, 1)
			go func() { served <- server.Serve(listener) }()
			close(started)
			<-callbackContext.Done()
			_ = server.Close()
			<-served
			return nil
		})
	}()
	<-started
	cancel()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("callback cancelled before runtime readback")
	}
	close(runtimeDone)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback listener did not join")
	}
}
