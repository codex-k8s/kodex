package websockettransport

import (
	"context"
	"errors"
	"testing"

	"github.com/coder/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExpectedDisconnect(t *testing.T) {
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "request context cancelled", ctx: cancelledContext, err: errors.New("publish failed"), want: true},
		{name: "context cancellation", ctx: context.Background(), err: context.Canceled, want: true},
		{name: "gRPC cancellation", ctx: context.Background(), err: status.Error(codes.Canceled, "caller cancelled"), want: true},
		{name: "normal close", ctx: context.Background(), err: websocket.CloseError{Code: websocket.StatusNormalClosure}, want: true},
		{name: "client going away", ctx: context.Background(), err: websocket.CloseError{Code: websocket.StatusGoingAway}, want: true},
		{name: "upstream unavailable", ctx: context.Background(), err: status.Error(codes.Unavailable, "upstream unavailable"), want: false},
		{name: "unexpected local failure", ctx: context.Background(), err: errors.New("projection failed"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := expectedDisconnect(test.ctx, test.err); got != test.want {
				t.Fatalf("expectedDisconnect() = %t, want %t", got, test.want)
			}
		})
	}
}
