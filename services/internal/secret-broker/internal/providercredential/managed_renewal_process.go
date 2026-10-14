package providercredential

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var errManagedRenewalUnknown = errors.New("managed provider credential renewal outcome is unknown")

func ManagedRenewalOutcomeUnknown(err error) bool { return errors.Is(err, errManagedRenewalUnknown) }

type managedRenewalAuth struct {
	Mode   string `json:"auth_mode"`
	APIKey string `json:"OPENAI_API_KEY"`
	Tokens *struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh time.Time `json:"last_refresh"`
}

func parseManagedRenewalAuth(raw []byte) (managedRenewalAuth, error) {
	var auth managedRenewalAuth
	if len(raw) < 1 || len(raw) > maximumAuthJSONBytes || json.Unmarshal(raw, &auth) != nil ||
		auth.Mode != "chatgpt" || auth.APIKey != "" || auth.Tokens == nil || auth.LastRefresh.IsZero() ||
		!validCatalogTokenField(auth.Tokens.AccessToken, 64<<10) ||
		!validCatalogTokenField(auth.Tokens.RefreshToken, 64<<10) ||
		!validCatalogTokenField(auth.Tokens.AccountID, 512) {
		return managedRenewalAuth{}, ErrInvalidInput
	}
	return auth, nil
}

// Отдельный kernel для WRITE-задачи владельца credential; observer его не вызывает.
// До входа task owner обязан durable-зафиксировать одноразовое разрешение эффекта.
// persist сохраняет произведённые bytes до удаления private home; неизвестный
// исход после запуска никогда не разрешает повтор OAuth со старым refresh token.
func (process *AppServerProcess) renewManagedCredential(ctx context.Context, original []byte, persist func(context.Context, []byte) error) (resultErr error) {
	if ctx == nil || persist == nil || ctx.Err() != nil {
		return ErrInvalidInput
	}
	before, err := parseManagedRenewalAuth(original)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, deviceAuthFinishBudget)
	defer cancel()
	if err := process.Check(ctx); err != nil {
		return err
	}
	home, err := os.MkdirTemp(process.root, "renewal-")
	if err != nil {
		return errors.New("create managed renewal state directory")
	}
	defer func() {
		if os.RemoveAll(home) != nil {
			resultErr = errManagedRenewalUnknown
		}
	}()
	seed, err := os.OpenFile(filepath.Join(home, "auth.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.New("create managed renewal credential file")
	}
	_, writeErr := seed.Write(original)
	closeErr := seed.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("write managed renewal credential file")
	}
	started := time.Now().UTC()
	server, err := startAppServer(process.binary, home)
	if err != nil {
		return errManagedRenewalUnknown
	}
	stop, joined := make(chan struct{}), make(chan struct{})
	var stopOnce sync.Once
	stopWatcher := func() { stopOnce.Do(func() { close(stop) }); <-joined }
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
		case <-stop:
		}
	}()
	defer func() {
		stopWatcher()
		if server.terminate() != nil {
			resultErr = errManagedRenewalUnknown
		}
	}()
	if _, err := server.call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "kodex-secret-broker", "title": "Kodex secret-broker", "version": "1"},
		"capabilities": map[string]bool{"experimentalApi": false},
	}); err != nil {
		return errManagedRenewalUnknown
	}
	if server.write(map[string]any{"method": "initialized"}) != nil {
		return errManagedRenewalUnknown
	}
	metadata, err := server.call(ctx, "account/read", map[string]bool{"refreshToken": true})
	if err != nil {
		return errManagedRenewalUnknown
	}
	defer clear(metadata)
	if _, err := maskedAccount(metadata); err != nil {
		return errManagedRenewalUnknown
	}
	// Прекращаем writer до exact file read: после чтения он не может сменить bytes.
	stopWatcher()
	if server.terminate() != nil {
		return errManagedRenewalUnknown
	}
	renewed, err := readAuthJSON(home)
	if err != nil {
		return errManagedRenewalUnknown
	}
	defer clear(renewed)
	after, err := parseManagedRenewalAuth(renewed)
	if err != nil || after.Tokens.AccountID != before.Tokens.AccountID ||
		after.Tokens.AccessToken == before.Tokens.AccessToken || after.LastRefresh.Before(started) ||
		after.LastRefresh.After(time.Now().UTC()) || ctx.Err() != nil {
		return errManagedRenewalUnknown
	}
	if persist(ctx, renewed) != nil {
		return errManagedRenewalUnknown
	}
	return nil
}
