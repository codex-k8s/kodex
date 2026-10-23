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

// errManagedRenewalUnknown marks an unproven managed OAuth outcome, including publication or cleanup
// uncertainty; callers may observe durable state but must not infer permission to refresh again.
// errManagedRenewalUnknown обозначает недоказанный managed OAuth исход, включая неопределённость
// публикации или cleanup; вызывающий может наблюдать durable состояние, но не выводить разрешение
// повторного refresh.
var errManagedRenewalUnknown = errors.New("managed provider credential renewal outcome is unknown")

// ManagedRenewalOutcomeUnknown recognizes the unknown-outcome sentinel through wrapped or joined errors
// without inspecting potentially sensitive error text.
// ManagedRenewalOutcomeUnknown распознаёт sentinel неопределённого исхода через wrapped или joined
// ошибки без анализа потенциально чувствительного текста.
func ManagedRenewalOutcomeUnknown(err error) bool { return errors.Is(err, errManagedRenewalUnknown) }

// managedRenewalAuth decodes the stable managed ChatGPT auth-file fields needed for refresh validation;
// token strings are private process data, not a transport response or a general canonical JSON schema.
// managedRenewalAuth разбирает стабильные поля managed ChatGPT auth-файла для проверки refresh; token
// strings являются приватными данными процесса, не transport ответом и не общей схемой канонического
// JSON.
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

// parseManagedRenewalAuth bounds input and requires managed chatgpt mode, no API key, complete bounded
// token fields and nonzero last_refresh. Standard JSON decoding accepts unknown fields and duplicates;
// freshness is checked only against the produced result later.
// parseManagedRenewalAuth ограничивает вход и требует managed chatgpt mode, отсутствие API key, полные
// ограниченные token поля и ненулевой last_refresh. Обычный JSON decoder допускает неизвестные и
// duplicate поля; актуальность позже проверяется по произведённому результату.
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

// renewManagedCredential runs only after the owner has durably reserved one effect. It seeds a private
// no-follow auth file, initializes the isolated process with experimental API disabled, requests refresh
// and stops the writer before exact result read. Account continuity, a changed access token and refresh
// time are verified before persist; post-start or cleanup uncertainty never permits replay.
// renewManagedCredential запускается только после durable резервирования одного эффекта владельцем. Он
// создаёт приватный auth-файл без следования symlink, инициализирует изолированный процесс с выключенным
// experimental API, запрашивает refresh и останавливает writer до точного чтения результата. До persist
// проверяются тот же account, смена access token и время refresh; неопределённость после запуска или
// cleanup не разрешает повтор.
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
