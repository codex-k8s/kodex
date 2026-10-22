// Package credentialrelay transfers provider authentication rotations from an isolated provider UID to the execution-bound callback owner using a private Unix socket.
// Пакет credentialrelay передаёт ротации авторизации от изолированного UID provider владельцу callback конкретного исполнения через приватный Unix-сокет.
package credentialrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// SocketPath is the fixed Unix socket shared by the isolated provider and credential relay; it is not a network endpoint or an execution grant.
	// SocketPath — фиксированный Unix-сокет изолированного provider и relay реквизитов; не сетевой endpoint и не grant исполнения.
	SocketPath = "/run/kodex/provider-credential-relay/relay.sock"
	// maximumRequestBytes allows one bounded runner input, base64-expanded authentication and fixed JSON overhead; the decoder reads at most this bound plus one byte.
	// maximumRequestBytes вмещает ограниченный вход runner, авторизацию после base64 и фиксированный JSON overhead; decoder читает не более предела плюс один байт.
	maximumRequestBytes = runtimecontract.MaximumRunnerInputBytes + ((runtimecontract.MaximumProviderAuthBytes + 2) / 3 * 4) + (64 << 10)
	// maximumAckBytes caps the entire acknowledgement before strict decoding, preventing an unbounded local peer response.
	// maximumAckBytes ограничивает весь ответ подтверждения до строгого разбора, не допуская неограниченного ответа локального peer.
	maximumAckBytes = 256
	// providerUID is the sole accepted sender UID from SO_PEERCRED; matching it does not replace input binding or server-owned grant checks.
	// providerUID — единственный допустимый UID отправителя из SO_PEERCRED; совпадение не заменяет привязку входа и серверные проверки grant.
	providerUID = 10002
	// relayUID is both the required relay process UID and the expected server peer UID checked by the provider client.
	// relayUID задаёт обязательный UID процесса relay и ожидаемый UID серверного peer при проверке клиентом provider.
	relayUID = 10003
	// shutdownDrain bounds post-cancellation acceptance and individual request processing so a final credential refresh can reach the authoritative owner without indefinite shutdown.
	// shutdownDrain ограничивает приём после отмены и обработку отдельного запроса, позволяя последнему refresh достичь авторитетного владельца без бесконечного завершения.
	shutdownDrain = 60 * time.Second
)

// request carries the candidate turn and typed credential refresh through the private socket; those caller-supplied fields are validated assertions, not independent authority.
// request переносит предлагаемый ход и типизированный refresh через приватный сокет; переданные клиентом поля — проверяемые утверждения, а не самостоятельные полномочия.
type request struct {
	Input   model.Input                                            `json:"input"`
	Refresh runtimecontract.RunnerProviderCredentialRefreshRequest `json:"refresh"`
}

// response acknowledges successful callback completion with one boolean only, without returning authentication bytes or private callback diagnostics.
// response подтверждает успешное завершение callback одним boolean без возврата байтов авторизации или приватной диагностики callback.
type response struct {
	OK bool `json:"ok"`
}

// refreshCommitter isolates the execution-scoped callback from socket framing so transport tests can use a synthetic owner without real credential mutation.
// refreshCommitter отделяет callback конкретного исполнения от обмена по сокету, позволяя тестам использовать синтетического владельца без изменения реальных реквизитов.
type refreshCommitter interface {
	// CommitProviderCredentialRefresh commits the supplied refresh through the execution-bound callback; the implementation retains authoritative ownership and grant validation.
	// CommitProviderCredentialRefresh фиксирует refresh через callback с привязкой исполнения; авторитетные проверки владения и grant остаются у реализации.
	CommitProviderCredentialRefresh(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error
}

// Commit validates and bounds one request, checks the Unix server UID, seals the write side and accepts only one bounded positive acknowledgement. Cancellation closes the connection; no retry or default deadline is added, and encoded buffers are cleared.
// Commit проверяет и ограничивает один запрос, сверяет UID Unix-сервера, закрывает сторону записи и принимает лишь одно ограниченное положительное подтверждение. Отмена закрывает соединение; повторы и собственный срок не добавляются, закодированные буферы очищаются.
func Commit(ctx context.Context, input model.Input, refresh runtimecontract.RunnerProviderCredentialRefreshRequest) error {
	payload := request{Input: input, Refresh: refresh}
	if _, err := validateRequest(input, payload); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) == 0 || len(raw) > maximumRequestBytes {
		clear(raw)
		return errors.New("encode provider credential relay request")
	}
	defer clear(raw)
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", SocketPath)
	if err != nil {
		return errors.New("provider credential relay is unavailable")
	}
	defer connection.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()
	uid, err := peerUID(connection)
	if err != nil || !authorizedRelayUID(uid) {
		return errors.New("provider credential relay peer is unauthorized")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if err := writeFull(connection, raw); err != nil {
		return errors.New("write provider credential relay request")
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok || unixConnection.CloseWrite() != nil {
		return errors.New("seal provider credential relay request")
	}
	ack, err := io.ReadAll(io.LimitReader(connection, maximumAckBytes+1))
	if err != nil || len(ack) == 0 || len(ack) > maximumAckBytes {
		return errors.New("read provider credential relay acknowledgement")
	}
	defer clear(ack)
	decoder := json.NewDecoder(bytes.NewReader(ack))
	decoder.DisallowUnknownFields()
	var result response
	if decoder.Decode(&result) != nil || !decodeEOF(decoder) || !result.OK {
		return errors.New("provider credential relay rejected request")
	}
	return nil
}

// Serve requires the dedicated relay UID, disables process dumpability, constructs the callback client and protects the fixed socket with group 29000 and mode 0660 before serving. It closes owned resources on return; grant revocation remains the owner's responsibility.
// Serve требует выделенный UID relay, запрещает dumpability процесса, создаёт callback-клиент и защищает фиксированный сокет группой 29000 и режимом 0660 до обслуживания. Закрывает свои ресурсы при выходе; отзыв grant остаётся обязанностью владельца.
func Serve(ctx context.Context, input model.Input) error {
	if os.Geteuid() != relayUID {
		return errors.New("provider credential relay UID is invalid")
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return errors.New("disable provider credential relay process inspection")
	}
	client, err := callback.New(input)
	if err != nil {
		return errors.New("create provider credential relay callback")
	}
	defer client.Close()
	if err := os.MkdirAll(filepath.Dir(SocketPath), 0o770); err != nil {
		return errors.New("create provider credential relay socket directory")
	}
	_ = os.Remove(SocketPath)
	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		return errors.New("listen provider credential relay socket")
	}
	defer listener.Close()
	if err := os.Chown(SocketPath, -1, 29000); err != nil || os.Chmod(SocketPath, 0o660) != nil {
		return errors.New("protect provider credential relay socket")
	}
	return serveListener(ctx, listener, shutdownDrain, func(requestContext context.Context, connection net.Conn) error {
		return serveConnection(requestContext, input, connection, client)
	})
}

// serveListener serially handles accepted connections using a detached serving context and per-request deadlines. On parent cancellation it allows a bounded final-refresh drain, then cancels active work, closes the listener and joins its watcher; cancellation alone does not revoke the grant, which the owner rechecks.
// serveListener последовательно обрабатывает соединения с отделённым контекстом и сроком на запрос. После отмены родителя оставляет ограниченное окно последнего refresh, затем отменяет активную работу, закрывает listener и дожидается watcher; сама отмена не отзывает grant, повторно проверяемый владельцем.
func serveListener(ctx context.Context, listener net.Listener, drain time.Duration, handle func(context.Context, net.Conn) error) error {
	serving, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	finished := make(chan struct{})
	watcherDone := make(chan struct{})
	defer func() { close(finished); <-watcherDone }()
	go func() {
		defer close(watcherDone)
		select {
		case <-finished:
			return
		case <-ctx.Done():
		}
		timer := time.NewTimer(drain)
		defer timer.Stop()
		select {
		case <-finished:
			return
		case <-timer.C:
			cancel()
			_ = listener.Close()
		}
	}()
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if serving.Err() != nil {
				return nil
			}
			return errors.New("accept provider credential relay request")
		}
		requestContext, cancelRequest := context.WithTimeout(serving, drain)
		_ = connection.SetDeadline(time.Now().Add(drain))
		stopClose := context.AfterFunc(requestContext, func() { _ = connection.Close() })
		_ = handle(requestContext, connection)
		stopClose()
		cancelRequest()
		_ = connection.Close()
	}
}

// serveConnection requires the socket peer to have the isolated provider UID before decoding any credential payload; binding and owner checks are separate subsequent stages.
// serveConnection требует у peer сокета UID изолированного provider до декодирования реквизитов; привязка и проверки владельца — отдельные последующие этапы.
func serveConnection(ctx context.Context, input model.Input, connection net.Conn, committer refreshCommitter) error {
	uid, err := peerUID(connection)
	if err != nil || !authorizedProviderUID(uid) {
		return errors.New("provider credential relay peer is unauthorized")
	}
	return serveBoundConnection(ctx, input, connection, committer)
}

// serveBoundConnection decodes one bounded request, clears decoded authentication on return, validates the candidate turn and acknowledges only after the committer succeeds. Peer UID enforcement belongs to its production caller.
// serveBoundConnection разбирает один ограниченный запрос, очищает декодированную авторизацию при выходе, проверяет ход и подтверждает лишь успешный committer. Проверка UID peer принадлежит production-вызывающему.
func serveBoundConnection(ctx context.Context, input model.Input, connection net.Conn, committer refreshCommitter) error {
	payload, err := decodeRequest(connection)
	if err != nil {
		return err
	}
	defer clear(payload.Refresh.Authentication)
	turn, err := validateRequest(input, payload)
	if err != nil {
		return err
	}
	if err := committer.CommitProviderCredentialRefresh(ctx, turn, payload.Refresh); err != nil {
		return errors.New("provider credential refresh callback failed")
	}
	if err := json.NewEncoder(connection).Encode(response{OK: true}); err != nil {
		return errors.New("write provider credential relay acknowledgement")
	}
	return nil
}

// decodeRequest bounds raw bytes, rejects unknown fields and trailing JSON values and clears raw or partially decoded authentication buffers on failure. It does not independently reject duplicate known JSON keys or validate execution authority.
// decodeRequest ограничивает сырые байты, отклоняет неизвестные поля и хвостовые JSON-значения, очищая сырые и частично декодированные реквизиты при ошибке. Сам не запрещает дубли известных JSON-ключей и не проверяет полномочия исполнения.
func decodeRequest(reader io.Reader) (request, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maximumRequestBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumRequestBytes {
		clear(raw)
		return request{}, errors.New("provider credential relay request exceeds its bound")
	}
	defer clear(raw)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload request
	if decoder.Decode(&payload) != nil || !decodeEOF(decoder) {
		clear(payload.Refresh.Authentication)
		return request{}, errors.New("provider credential relay request is invalid")
	}
	return payload, nil
}

// validateRequest checks valid turn/refresh shapes and prior credential pins. A bound turn requires its exact lease/fence/generation/revision digest; warm mode additionally requires system scope, workload, callback/ticket path and compatibility digest. This local comparison does not replace the callback owner's grant validation.
// validateRequest проверяет корректные формы хода/refresh и прежние pins реквизитов. Для заданного хода требует точные lease/fence/generation/дайджест ревизии; warm дополнительно требует системную область, workload, callback/путь ticket и дайджест совместимости. Локальное сравнение не заменяет проверку grant владельцем callback.
func validateRequest(bound model.Input, payload request) (model.Input, error) {
	turn := payload.Input
	if turn.Mode != runtimecontract.RunnerModeTurn || turn.Validate() != nil || payload.Refresh.Validate() != nil ||
		payload.Refresh.RuntimeRevisionDigest != turn.RuntimeRevisionDigest ||
		payload.Refresh.PreviousCredentialRevisionRef != turn.ProviderCredentialRef ||
		payload.Refresh.PreviousContentSHA256 != turn.ProviderCredentialSHA256 {
		return model.Input{}, errors.New("provider credential relay binding is invalid")
	}
	if bound.Mode == runtimecontract.RunnerModeTurn {
		if bound.LeaseRef != turn.LeaseRef || bound.LeaseFence != turn.LeaseFence ||
			bound.LeaseGeneration != turn.LeaseGeneration || bound.RuntimeRevisionDigest != turn.RuntimeRevisionDigest {
			return model.Input{}, errors.New("provider credential relay execution binding is invalid")
		}
		return turn, nil
	}
	if bound.Mode != runtimecontract.RunnerModeWarm || !bound.IsSystemAssistant() || !turn.IsSystemAssistant() ||
		bound.WorkloadInstance != turn.WorkloadInstance || bound.CallbackURL != turn.CallbackURL ||
		bound.CallbackTLS != turn.CallbackTLS || bound.ExecutionTicketFile != turn.ExecutionTicketFile {
		return model.Input{}, errors.New("provider credential relay warm binding is invalid")
	}
	boundCompatibility, boundErr := runtimecontract.WarmCompatibilityDigest(bound)
	turnCompatibility, turnErr := runtimecontract.WarmCompatibilityDigest(turn)
	if boundErr != nil || turnErr != nil || boundCompatibility != turnCompatibility {
		return model.Input{}, errors.New("provider credential relay warm compatibility is invalid")
	}
	return turn, nil
}

// peerUID accepts only a Unix connection and reads the kernel SO_PEERCRED UID through SyscallConn.Control; unsupported transport or inspection failure returns a fixed diagnostic without credential data.
// peerUID принимает только Unix-соединение и читает kernel UID SO_PEERCRED через SyscallConn.Control; чужой транспорт или ошибка проверки возвращают фиксированную диагностику без реквизитов.
func peerUID(connection net.Conn) (uint32, error) {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return 0, errors.New("provider credential relay transport is invalid")
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return 0, errors.New("inspect provider credential relay peer")
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || controlErr != nil || credential == nil {
		return 0, errors.New("inspect provider credential relay peer")
	}
	return credential.Uid, nil
}

// authorizedProviderUID allows only the dedicated provider UID, excluding both the runner and relay identities without consulting payload fields.
// authorizedProviderUID разрешает только выделенный UID provider, исключая runner и relay независимо от полей пакета.
func authorizedProviderUID(uid uint32) bool {
	return uid == providerUID
}

// authorizedRelayUID allows only the dedicated relay UID when authenticating the server end of the private socket.
// authorizedRelayUID разрешает только выделенный UID relay при проверке серверной стороны приватного сокета.
func authorizedRelayUID(uid uint32) bool {
	return uid == relayUID
}

// writeFull advances through partial successful writes, propagates writer errors and rejects zero, negative or oversized progress with io.ErrShortWrite instead of looping indefinitely.
// writeFull продвигается при частичных успешных записях, возвращает ошибки writer и отклоняет нулевой, отрицательный или чрезмерный прогресс через io.ErrShortWrite вместо бесконечного цикла.
func writeFull(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(payload) {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}

// decodeEOF accepts only end of input after the expected JSON value; a second value or malformed trailing data fails the framing check.
// decodeEOF принимает только конец входа после ожидаемого JSON-значения; второе значение или повреждённый хвост нарушают framing.
func decodeEOF(decoder *json.Decoder) bool {
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}
