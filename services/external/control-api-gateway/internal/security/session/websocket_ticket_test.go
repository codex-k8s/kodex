package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
)

func TestWebSocketTicketReplicaCASAndIndependentTabs(t *testing.T) {
	families, family, _ := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		t.Fatal("unexpected provider call")
		return oidcverifier.BrowserTokens{}, nil
	}))
	first, expires, err := families.IssueWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
	if err != nil || len(first) != 43 || expires.Sub(families.now()) != websocketTicketLifetime {
		t.Fatal("ticket issuance failed", err)
	}
	replica := *families
	second, _, err := replica.IssueWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
	if err != nil || first == second {
		t.Fatal("second tab did not get independent ticket")
	}
	current, err := families.Read(t.Context(), family.ID)
	if err != nil || current.Version != family.Version || current.Sequence != family.Sequence {
		t.Fatal("ticket mutated session version")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, gateway := range []*Families{families, &replica} {
		wg.Go(func() {
			if gateway.ConsumeWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash, first) == nil {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("ticket was not consumed exactly once")
	}
	if err := replica.ConsumeWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash, second); err != nil {
		t.Fatal("first tab invalidated second ticket", err)
	}
	if err := families.ConsumeWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash, second); !errors.Is(err, ErrReauthentication) {
		t.Fatal("restart replay was accepted")
	}
}

func TestWebSocketTicketExpiryBindingBoundsAndRevocation(t *testing.T) {
	for _, scenario := range []string{"expired", "browser", "csrf", "version", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			families, family, _ := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
				return oidcverifier.BrowserTokens{}, nil
			}))
			token, expires, err := families.IssueWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
			if err != nil {
				t.Fatal(err)
			}
			browserID, csrf := family.BrowserSessionID, family.CSRFHash
			switch scenario {
			case "expired":
				families.now = func() time.Time { return expires }
			case "browser":
				browserID = "another-browser"
			case "csrf":
				csrf = "another-csrf"
			case "version":
				family.Version++
				if _, err := families.write(t.Context(), family); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := families.Revoke(t.Context(), family.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := families.ConsumeWebSocketTicket(t.Context(), family.ID, browserID, csrf, token); !errors.Is(err, ErrReauthentication) {
				t.Fatal("invalid ticket accepted", err)
			}
		})
	}
	families, family, _ := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		return oidcverifier.BrowserTokens{}, nil
	}))
	for range maximumWebSocketTickets {
		if _, _, err := families.IssueWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := families.IssueWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); !errors.Is(err, ErrRenewalPending) {
		t.Fatal("unbounded ticket issuance")
	}
	next := families.now().Add(websocketTicketLifetime)
	families.now = func() time.Time { return next }
	if _, _, err := families.IssueWebSocketTicket(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); err != nil {
		t.Fatal("expired tickets did not release capacity", err)
	}
}
