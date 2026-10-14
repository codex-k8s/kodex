package providercredential

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func renewalFixture(account string) []byte {
	raw, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{
		"account_id": account, "access_token": "synthetic-old-access", "refresh_token": "synthetic-old-refresh",
	}, "last_refresh": time.Now().UTC().Add(-time.Hour)})
	return raw
}

func runManagedRenewalFixture(home string, raw []byte) int {
	auth, err := parseManagedRenewalAuth(raw)
	if err != nil {
		return 20
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 21
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			var params struct {
				Capabilities struct {
					Experimental bool `json:"experimentalApi"`
				} `json:"capabilities"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.Capabilities.Experimental {
				return 22
			}
		case "initialized":
			continue
		case "account/read":
			var params struct {
				Refresh bool `json:"refreshToken"`
			}
			if json.Unmarshal(request.Params, &params) != nil || !params.Refresh {
				return 23
			}
			result = map[string]any{"account": map[string]any{"type": "chatgpt", "email": nil}}
			if auth.Tokens.AccountID != "fixture-no-refresh" {
				auth.Tokens.AccessToken = "synthetic-new-access"
				auth.Tokens.RefreshToken = "synthetic-new-refresh"
				auth.LastRefresh = time.Now().UTC()
			}
			if auth.Tokens.AccountID == "fixture-foreign" {
				auth.Tokens.AccountID = "other-account"
			}
			updated, _ := json.Marshal(auth)
			if os.WriteFile(filepath.Join(home, "auth.json"), updated, 0o600) != nil {
				return 24
			}
		default:
			return 25
		}
		if json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request.ID, "result": result}) != nil {
			return 26
		}
	}
	return 0
}

func TestManagedRenewalProcessStableAuthAndDurableSink(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"fixture-success", "fixture-no-refresh", "fixture-foreign", "fixture-store-failure"} {
		t.Run(account, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			process, err := NewAppServerProcess(binary, root, false)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = process.renewManagedCredential(t.Context(), renewalFixture(account), func(ctx context.Context, raw []byte) error {
				calls++
				updated, parseErr := parseManagedRenewalAuth(raw)
				if ctx.Err() != nil || parseErr != nil || updated.Tokens.AccountID != account || updated.Tokens.AccessToken != "synthetic-new-access" {
					t.Fatal("sink received invalid credential")
				}
				if account == "fixture-store-failure" {
					return errors.New("synthetic-private-store-failure")
				}
				return nil
			})
			if account == "fixture-success" {
				if err != nil || calls != 1 {
					t.Fatal("managed renewal did not persist exactly once")
				}
			} else {
				wantCalls := 0
				if account == "fixture-store-failure" {
					wantCalls = 1
				}
				if !errors.Is(err, errManagedRenewalUnknown) || calls != wantCalls || strings.Contains(err.Error(), "synthetic") {
					t.Fatal("unknown renewal was accepted or exposed")
				}
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil || len(entries) != 0 {
				t.Fatal("renewal retained private process state")
			}
		})
	}
}

func TestManagedRenewalRejectsExternalOrIncompleteAuth(t *testing.T) {
	for _, raw := range []string{`{}`, `{"auth_mode":"chatgptAuthTokens"}`, `{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic"}`} {
		if _, err := parseManagedRenewalAuth([]byte(raw)); !errors.Is(err, ErrInvalidInput) || strings.Contains(err.Error(), "synthetic") {
			t.Fatal("invalid managed auth accepted or exposed")
		}
	}
}
