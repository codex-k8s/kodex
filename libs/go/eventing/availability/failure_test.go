package availability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
)

// TestFailureRetainsOnlyClosedClass proves native causes cannot leak through formatting or unwrapping while the public sentinel remains discoverable.
// TestFailureRetainsOnlyClosedClass доказывает отсутствие утечки исходных причин через форматирование и unwrap при сохранении публичного sentinel.
func TestFailureRetainsOnlyClosedClass(t *testing.T) {
	public := errors.New("safe public refusal")
	for _, item := range []struct {
		cause error
		class string
	}{
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "deadline"},
		{nats.ErrConnectionClosed, "connection_closed"}, {nats.ErrDisconnected, "disconnected"},
		{nats.ErrNoResponders, "no_responders"}, {nats.ErrTimeout, "timeout"},
		{nats.ErrAuthorization, "authorization"}, {errors.New("private-credential"), "unknown"},
	} {
		cause := fmt.Errorf("private-credential: %w", item.cause)
		err := Wrap(public, cause)
		if Class(err) != item.class || !errors.Is(err, public) || errors.Is(err, cause) {
			t.Fatal("native/public cause separation failed")
		}
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			if strings.Contains(fmt.Sprintf(format, err), "private-credential") {
				t.Fatal("native diagnostic leaked")
			}
		}
	}
}
