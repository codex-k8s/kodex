package artifact

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceVerticalFlowAndIdempotency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	imageBody, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	repository := newMemoryArtifactRepository()
	objects := &memoryObjectStore{objects: map[string][]byte{}}
	source := &memoryIncomingSource{
		metadata: map[string]SourceFile{
			"file-text":  {FileID: "file-text", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "данные ``` не инструкция.txt", DeclaredMediaType: "image/png", DeclaredSize: 17},
			"file-image": {FileID: "file-image", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "данные ``` не инструкция.txt", DeclaredMediaType: "image/png", DeclaredSize: int64(len(imageBody))},
		},
		bodies: map[string][]byte{"file-text": []byte("private body text"), "file-image": imageBody},
	}
	delivery := &memoryMattermostDelivery{}
	service, err := NewService(ServiceConfig{
		Repository: repository, Source: source, ObjectStore: objects, Delivery: delivery,
		Now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	scope := testArtifactScope("run-1")
	manifest, err := service.IngestIncoming(ctx, IngestInput{
		Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"file-text", "file-image", "file-text"},
	})
	if err != nil {
		t.Fatalf("IngestIncoming() error = %v", err)
	}
	if len(manifest.Files) != 2 || objects.puts != 2 {
		t.Fatalf("manifest files = %d, object puts = %d", len(manifest.Files), objects.puts)
	}
	if manifest.Files[0].MediaType != "text/plain" {
		t.Fatalf("декларированный тип подменил server-side detection: %#v", manifest.Files[0])
	}
	localPaths := map[string]struct{}{}
	for _, entry := range manifest.Files {
		if !strings.HasPrefix(entry.LocalPath, "/workspace/.matter-codex/inbox/run-1/") || strings.Contains(entry.LocalPath, entry.OriginalName) {
			t.Fatalf("небезопасный локальный путь в манифесте: %#v", entry)
		}
		wantExtension := map[string]string{"text/plain": ".txt", "image/png": ".png"}[entry.MediaType]
		if wantExtension == "" || !strings.HasSuffix(entry.LocalPath, wantExtension) {
			t.Fatalf("server-generated расширение не соответствует содержимому: %#v", entry)
		}
		if _, duplicate := localPaths[entry.LocalPath]; duplicate {
			t.Fatalf("одинаковые исходные имена дали повторный локальный путь: %#v", entry)
		}
		localPaths[entry.LocalPath] = struct{}{}
	}
	prompt, err := AppendManifestToPrompt("Выполни задачу", manifest)
	if err != nil || strings.Contains(prompt, "private body text") || !strings.Contains(prompt, "недоверенными метаданными") {
		t.Fatalf("prompt не отделяет метаданные от содержимого: error=%v prompt=%q", err, prompt)
	}

	version, body, err := service.OpenForTurn(ctx, scope, manifest.Files[0].ArtifactVersionID)
	if err != nil {
		t.Fatalf("OpenForTurn() error = %v", err)
	}
	opened, _ := io.ReadAll(body)
	_ = body.Close()
	if int64(len(opened)) != version.Size {
		t.Fatalf("opened size = %d, want %d", len(opened), version.Size)
	}
	foreignScope := scope
	foreignScope.SessionID++
	if _, _, err := service.OpenForTurn(ctx, foreignScope, version.VersionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign scope error = %v, want ErrNotFound", err)
	}

	result, err := service.PublishOutgoing(ctx, PublishInput{
		Scope: scope, IdempotencyKey: "answer-v1", OriginalName: "ответ.txt", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader("safe answer"),
	})
	if err != nil || result.State != DeliveryDelivered {
		t.Fatalf("PublishOutgoing() result=%#v error=%v", result, err)
	}
	repeated, err := service.PublishOutgoing(ctx, PublishInput{
		Scope: scope, IdempotencyKey: "answer-v1", OriginalName: "другое имя.txt", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader("different body"),
	})
	if err != nil || repeated.DeliveryID != result.DeliveryID || delivery.uploads != 1 || delivery.publishes != 1 || objects.puts != 3 {
		t.Fatalf("идемпотентность нарушена: repeated=%#v error=%v uploads=%d publishes=%d puts=%d", repeated, err, delivery.uploads, delivery.publishes, objects.puts)
	}
	delivery.failPublishOnce = true
	if _, err := service.PublishOutgoing(ctx, PublishInput{
		Scope: scope, IdempotencyKey: "answer-retry", OriginalName: "retry.txt", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader("retry body"),
	}); err == nil {
		t.Fatal("первая неоднозначная публикация должна вернуть ошибку")
	}
	retried, err := service.PublishOutgoing(ctx, PublishInput{
		Scope: scope, IdempotencyKey: "answer-retry", OriginalName: "ignored.txt", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader("ignored body"),
	})
	if err != nil || retried.State != DeliveryDelivered || delivery.uploads != 2 || delivery.publishes != 3 || objects.puts != 4 {
		t.Fatalf("повтор публикации создал лишний файл: result=%#v error=%v uploads=%d publishes=%d puts=%d", retried, err, delivery.uploads, delivery.publishes, objects.puts)
	}

	retryScope := scope
	retryScope.TurnID = "run-2"
	retryManifest, err := service.IngestIncoming(ctx, IngestInput{
		Scope: retryScope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"file-text", "file-image"},
	})
	if err != nil || len(retryManifest.Files) != 2 || objects.puts != 4 {
		t.Fatalf("повторный inbound event создал новый объект: files=%d puts=%d error=%v", len(retryManifest.Files), objects.puts, err)
	}
}

func TestServiceUsesCanonicalMarkdownAndCSVForIngestAndPublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := newMemoryArtifactRepository()
	objects := &memoryObjectStore{objects: map[string][]byte{}}
	source := &memoryIncomingSource{
		metadata: map[string]SourceFile{
			"csv": {FileID: "csv", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "неверное.exe", DeclaredMediaType: "application/octet-stream"},
			"md":  {FileID: "md", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "неверное.csv", DeclaredMediaType: "text/csv"},
		},
		bodies: map[string][]byte{
			"csv": []byte("имя,значение\nальфа,1\nбета,2\n"),
			"md":  []byte("# Отчёт\n\nПроверяемый текст.\n"),
		},
	}
	delivery := &memoryMattermostDelivery{}
	service, err := NewService(ServiceConfig{Repository: repository, Source: source, ObjectStore: objects, Delivery: delivery})
	if err != nil {
		t.Fatal(err)
	}
	scope := testArtifactScope("run-content-text")
	manifest, err := service.IngestIncoming(ctx, IngestInput{
		Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"csv", "md"},
	})
	if err != nil {
		t.Fatalf("IngestIncoming() error = %v", err)
	}
	wantExtensions := map[string]string{"text/csv": ".csv", "text/markdown": ".md"}
	if len(manifest.Files) != len(wantExtensions) {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, entry := range manifest.Files {
		extension, ok := wantExtensions[entry.MediaType]
		if !ok || !strings.HasSuffix(entry.LocalPath, extension) {
			t.Fatalf("ingest не использовал канонический MIME/extension: %#v", entry)
		}
	}
	for key, body := range map[string]string{
		"csv-output": "имя,значение\nальфа,1\nбета,2\n",
		"md-output":  "# Итог\n\nПроверяемый результат.\n",
	} {
		result, err := service.PublishOutgoing(ctx, PublishInput{
			Scope: scope, IdempotencyKey: key, OriginalName: "недоверенное.bin", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader(body),
		})
		if err != nil || result.State != DeliveryDelivered {
			t.Fatalf("PublishOutgoing(%s) result=%#v error=%v", key, result, err)
		}
	}
	outboundTypes := map[string]bool{}
	for _, version := range repository.versions {
		if version.Direction == DirectionOutbound {
			outboundTypes[version.MediaType] = true
		}
	}
	if !outboundTypes["text/csv"] || !outboundTypes["text/markdown"] || delivery.uploads != 2 || delivery.publishes != 2 {
		t.Fatalf("publish не сохранил канонические text formats: types=%#v uploads=%d publishes=%d", outboundTypes, delivery.uploads, delivery.publishes)
	}
}

func TestServiceRejectsLimitsMediaAndQuarantinesSecrets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := newMemoryArtifactRepository()
	objects := &memoryObjectStore{objects: map[string][]byte{}}
	source := &memoryIncomingSource{metadata: map[string]SourceFile{}, bodies: map[string][]byte{}}
	for index := 0; index < DefaultMaxFilesPerTurn+1; index++ {
		id := string(rune('a' + index))
		source.metadata[id] = SourceFile{FileID: id, PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: id + ".txt", DeclaredSize: 1}
		source.bodies[id] = []byte("x")
	}
	service, err := NewService(ServiceConfig{Repository: repository, Source: source, ObjectStore: objects, Delivery: &memoryMattermostDelivery{}})
	if err != nil {
		t.Fatal(err)
	}
	scope := testArtifactScope("run-limits")
	tooMany := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	if _, err := service.IngestIncoming(ctx, IngestInput{Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: tooMany}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("file count error = %v", err)
	}
	source.metadata["foreign"] = SourceFile{FileID: "foreign", PostID: "post-1", ChannelID: "other-channel", CreatorID: "user-1", OriginalName: "foreign.txt", DeclaredSize: 1}
	source.bodies["foreign"] = []byte("x")
	if _, err := service.IngestIncoming(ctx, IngestInput{Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"foreign"}}); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("foreign source scope error = %v", err)
	}

	source.metadata["oversized"] = SourceFile{FileID: "oversized", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "large.txt", DeclaredSize: DefaultMaxObjectBytes}
	source.bodies["oversized"] = bytes.Repeat([]byte("x"), int(DefaultMaxObjectBytes+1))
	if _, err := service.IngestIncoming(ctx, IngestInput{Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"oversized"}}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversized error = %v", err)
	}

	source.metadata["binary"] = SourceFile{FileID: "binary", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "archive.zip", DeclaredSize: 8}
	source.bodies["binary"] = []byte{'P', 'K', 3, 4, 0, 0, 0, 0}
	if _, err := service.IngestIncoming(ctx, IngestInput{Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"binary"}}); !errors.Is(err, ErrMediaTypeDenied) {
		t.Fatalf("media type error = %v", err)
	}

	source.metadata["secret"] = SourceFile{FileID: "secret", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: ".env", DeclaredSize: 40}
	source.bodies["secret"] = []byte("api_key=abcdefghijklmnop1234567890")
	if _, err := service.IngestIncoming(ctx, IngestInput{Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"secret"}}); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("secret inbound error = %v", err)
	}
	if objects.puts != 0 {
		t.Fatalf("карантинный inbound объект записан: puts=%d", objects.puts)
	}

	result, err := service.PublishOutgoing(ctx, PublishInput{
		Scope: scope, IdempotencyKey: "secret-output", OriginalName: "output.txt", BotTokenSecretRef: "role-bot-secret",
		Body: strings.NewReader("password=abcdefghijklmnop1234567890"),
	})
	if !errors.Is(err, ErrQuarantined) || !result.Quarantined || objects.puts != 0 {
		t.Fatalf("secret outbound result=%#v error=%v puts=%d", result, err, objects.puts)
	}
}

func TestServiceEnforcesCumulativeTurnLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scope := testArtifactScope("run-cumulative-limits")
	source := &memoryIncomingSource{
		metadata: map[string]SourceFile{
			"one":   {FileID: "one", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "one.txt", DeclaredSize: 1},
			"two":   {FileID: "two", PostID: "post-2", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "two.txt", DeclaredSize: 1},
			"three": {FileID: "three", PostID: "post-3", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "three.txt", DeclaredSize: 1},
		},
		bodies: map[string][]byte{"one": []byte("1"), "two": []byte("2"), "three": []byte("3")},
	}
	service, err := NewService(ServiceConfig{
		Repository: newMemoryArtifactRepository(), Source: source,
		ObjectStore: &memoryObjectStore{objects: map[string][]byte{}}, Delivery: &memoryMattermostDelivery{},
		MaxFilesPerTurn: 2, MaxObjectBytes: 8, MaxTurnBytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ postID, fileID string }{{"post-1", "one"}, {"post-2", "two"}} {
		if _, err := service.IngestIncoming(ctx, IngestInput{Scope: scope, SourcePostID: item.postID, SourceUserID: "user-1", FileIDs: []string{item.fileID}}); err != nil {
			t.Fatalf("incremental inbound %s error = %v", item.fileID, err)
		}
	}
	if _, err := service.IngestIncoming(ctx, IngestInput{Scope: scope, SourcePostID: "post-3", SourceUserID: "user-1", FileIDs: []string{"three"}}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("cumulative file count error = %v", err)
	}

	publishService, err := NewService(ServiceConfig{
		Repository: newMemoryArtifactRepository(), ObjectStore: &memoryObjectStore{objects: map[string][]byte{}},
		Delivery: &memoryMattermostDelivery{}, MaxFilesPerTurn: 8, MaxObjectBytes: 8, MaxTurnBytes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publishService.PublishOutgoing(ctx, PublishInput{
		Scope: scope, IdempotencyKey: "six-bytes", OriginalName: "six.txt", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader("123456"),
	}); err != nil {
		t.Fatalf("first outbound error = %v", err)
	}
	if _, err := publishService.PublishOutgoing(ctx, PublishInput{
		Scope: scope, IdempotencyKey: "five-bytes", OriginalName: "five.txt", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader("12345"),
	}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("cumulative byte limit error = %v", err)
	}
}

func TestServiceRecoversMissingImmutableObjectWithoutNewVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := newMemoryArtifactRepository()
	objects := &memoryObjectStore{objects: map[string][]byte{}, failBefore: 1}
	source := &memoryIncomingSource{
		metadata: map[string]SourceFile{"file-1": {
			FileID: "file-1", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "input.txt", DeclaredSize: 5,
		}},
		bodies: map[string][]byte{"file-1": []byte("input")},
	}
	delivery := &memoryMattermostDelivery{}
	service, err := NewService(ServiceConfig{Repository: repository, Source: source, ObjectStore: objects, Delivery: delivery})
	if err != nil {
		t.Fatal(err)
	}
	scope := testArtifactScope("run-object-recovery")
	input := IngestInput{Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"file-1"}}
	if _, err := service.IngestIncoming(ctx, input); err == nil {
		t.Fatal("первая входящая запись должна имитировать отказ до создания объекта")
	}
	manifest, err := service.IngestIncoming(ctx, input)
	if err != nil || len(manifest.Files) != 1 || len(repository.versions) != 1 || objects.puts != 1 {
		t.Fatalf("inbound recovery manifest=%#v versions=%d puts=%d error=%v", manifest, len(repository.versions), objects.puts, err)
	}

	objects.failBefore = 1
	publish := PublishInput{
		Scope: scope, IdempotencyKey: "recover-output", OriginalName: "output.txt", BotTokenSecretRef: "role-bot-secret", Body: strings.NewReader("answer"),
	}
	if _, err := service.PublishOutgoing(ctx, publish); err == nil {
		t.Fatal("первая исходящая запись должна имитировать отказ до создания объекта")
	}
	publish.Body = strings.NewReader("changed")
	if _, err := service.PublishOutgoing(ctx, publish); !errors.Is(err, ErrConflict) {
		t.Fatalf("изменённое тело под прежним ключом error = %v", err)
	}
	publish.Body = strings.NewReader("answer")
	result, err := service.PublishOutgoing(ctx, publish)
	if err != nil || result.State != DeliveryDelivered || len(repository.versions) != 2 || objects.puts != 2 || delivery.uploads != 1 || delivery.publishes != 1 {
		t.Fatalf("outbound recovery result=%#v versions=%d puts=%d uploads=%d publishes=%d error=%v", result, len(repository.versions), objects.puts, delivery.uploads, delivery.publishes, err)
	}
}

func TestServiceRecoversPartialInboundBatchWithoutOrphanOrDuplicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := newMemoryArtifactRepository()
	objects := &memoryObjectStore{objects: map[string][]byte{}, failAtAttempt: 2}
	source := &memoryIncomingSource{
		metadata: map[string]SourceFile{
			"file-1": {FileID: "file-1", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "one.txt", DeclaredSize: 3},
			"file-2": {FileID: "file-2", PostID: "post-1", ChannelID: "channel-1", CreatorID: "user-1", OriginalName: "two.txt", DeclaredSize: 3},
		},
		bodies: map[string][]byte{"file-1": []byte("one"), "file-2": []byte("two")},
	}
	service, err := NewService(ServiceConfig{Repository: repository, Source: source, ObjectStore: objects, Delivery: &memoryMattermostDelivery{}})
	if err != nil {
		t.Fatal(err)
	}
	scope := testArtifactScope("run-partial-batch")
	input := IngestInput{Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"file-1", "file-2"}}
	if _, err := service.IngestIncoming(ctx, input); err == nil {
		t.Fatal("частичный batch должен вернуть ошибку второй object-store записи")
	}
	if len(repository.versions) != 2 || len(objects.objects) != 1 {
		t.Fatalf("частичный batch потерял recoverable state: versions=%d objects=%d", len(repository.versions), len(objects.objects))
	}
	for versionID, version := range repository.versions {
		if _, bound := repository.bindings[versionID][scope.TurnID]; !bound {
			t.Fatalf("retention-held version осталась без durable turn binding: %#v", version)
		}
		if version.State != StateAvailable && version.State != StateScanning {
			t.Fatalf("неожиданное recoverable state: %#v", version)
		}
	}
	manifest, err := service.IngestIncoming(ctx, input)
	if err != nil {
		t.Fatalf("повтор partial batch error = %v", err)
	}
	if len(manifest.Files) != 2 || len(repository.versions) != 2 || len(objects.objects) != 2 || objects.puts != 2 {
		t.Fatalf("повтор создал дубликат: manifest=%#v versions=%d objects=%d puts=%d", manifest, len(repository.versions), len(objects.objects), objects.puts)
	}
	for versionID, turns := range repository.bindings {
		if len(turns) != 1 {
			t.Fatalf("version %s имеет дублирующиеся bindings: %#v", versionID, turns)
		}
	}
}

func testArtifactScope(turnID string) Scope {
	return Scope{
		ProjectID: 1, ChatID: 2, SessionID: 3, RoleID: 4, RuntimeTurnID: 5, TurnID: turnID, SessionKey: "session-1",
		MattermostChannelID: "channel-1", MattermostRootPostID: "root-1",
	}
}

type memoryIncomingSource struct {
	metadata map[string]SourceFile
	bodies   map[string][]byte
}

func (source *memoryIncomingSource) Metadata(_ context.Context, fileID string) (SourceFile, error) {
	metadata, ok := source.metadata[fileID]
	if !ok {
		return SourceFile{}, ErrNotFound
	}
	return metadata, nil
}

func (source *memoryIncomingSource) Open(_ context.Context, fileID string) (io.ReadCloser, error) {
	body, ok := source.bodies[fileID]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

type memoryObjectStore struct {
	mu            sync.Mutex
	objects       map[string][]byte
	puts          int
	attempts      int
	failBefore    int
	failAtAttempt int
}

func (store *memoryObjectStore) PutImmutable(_ context.Context, key string, _ string, size int64, sha256Text string, body io.Reader) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.attempts++
	if store.failBefore > 0 {
		store.failBefore--
		return errors.New("synthetic object store failure before write")
	}
	if store.failAtAttempt > 0 && store.attempts == store.failAtAttempt {
		return errors.New("synthetic object store failure at exact attempt")
	}
	if _, exists := store.objects[key]; exists {
		return ErrConflict
	}
	value, err := io.ReadAll(body)
	if err != nil || int64(len(value)) != size {
		return ErrConflict
	}
	store.objects[key] = value
	store.puts++
	_ = sha256Text
	return nil
}

func (store *memoryObjectStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), value...))), nil
}

type memoryMattermostDelivery struct {
	uploads         int
	publishes       int
	failPublishOnce bool
}

func (delivery *memoryMattermostDelivery) Upload(_ context.Context, request DeliveryRequest, body io.Reader) (string, error) {
	delivery.uploads++
	value, err := io.ReadAll(body)
	if err != nil || int64(len(value)) != request.Size {
		return "", ErrConflict
	}
	return "file-delivered", nil
}

func (delivery *memoryMattermostDelivery) Publish(_ context.Context, _ DeliveryRequest, fileID string) (DeliveryReceipt, error) {
	delivery.publishes++
	if delivery.failPublishOnce {
		delivery.failPublishOnce = false
		return DeliveryReceipt{}, errors.New("synthetic ambiguous Mattermost response")
	}
	return DeliveryReceipt{MattermostFileID: "file-delivered", MattermostPostID: "post-delivered"}, nil
}

type memoryArtifactRepository struct {
	mu         sync.Mutex
	versions   map[string]Version
	inbound    map[string]string
	bindings   map[string]map[string]int
	deliveries map[string]Delivery
}

func newMemoryArtifactRepository() *memoryArtifactRepository {
	return &memoryArtifactRepository{
		versions: map[string]Version{}, inbound: map[string]string{}, bindings: map[string]map[string]int{}, deliveries: map[string]Delivery{},
	}
}

func (repo *memoryArtifactRepository) FindInbound(_ context.Context, scope Scope, postID string, fileID string) (Version, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	versionID, ok := repo.inbound[inboundMemoryKey(scope, postID, fileID)]
	if !ok {
		return Version{}, ErrNotFound
	}
	version := repo.versions[versionID]
	version.Scope = scope
	return version, nil
}

func (repo *memoryArtifactRepository) CreateInbound(_ context.Context, input CreateVersionInput) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	key := inboundMemoryKey(input.Version.Scope, input.Version.SourcePostID, input.Version.SourceFileID)
	if _, exists := repo.inbound[key]; exists {
		return ErrConflict
	}
	repo.versions[input.Version.VersionID] = input.Version
	repo.inbound[key] = input.Version.VersionID
	bindMemory(repo.bindings, input.Version.VersionID, input.Version.Scope.TurnID, input.Version.Ordinal)
	return nil
}

func (repo *memoryArtifactRepository) BindInbound(_ context.Context, versionID string, scope Scope, _ string, _ string, ordinal int) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if _, ok := repo.versions[versionID]; !ok {
		return ErrNotFound
	}
	bindMemory(repo.bindings, versionID, scope.TurnID, ordinal)
	return nil
}

func (repo *memoryArtifactRepository) ListTurn(_ context.Context, scope Scope) ([]Version, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	var result []Version
	for versionID, turns := range repo.bindings {
		ordinal, ok := turns[scope.TurnID]
		if !ok {
			continue
		}
		version := repo.versions[versionID]
		if version.Scope.ProjectID != scope.ProjectID || version.Scope.ChatID != scope.ChatID || version.Scope.SessionID != scope.SessionID {
			continue
		}
		version.Scope = scope
		version.Ordinal = ordinal
		result = append(result, version)
	}
	return result, nil
}

func (repo *memoryArtifactRepository) GetAvailable(_ context.Context, scope Scope, versionID string) (Version, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	version, ok := repo.versions[versionID]
	_, bound := repo.bindings[versionID][scope.TurnID]
	if !ok || !bound || version.Scope.ProjectID != scope.ProjectID || version.Scope.ChatID != scope.ChatID || version.Scope.SessionID != scope.SessionID || version.State != StateAvailable {
		return Version{}, ErrNotFound
	}
	version.Scope = scope
	return version, nil
}

func (repo *memoryArtifactRepository) SetVersionState(_ context.Context, versionID string, from VersionState, to VersionState, _ string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	version, ok := repo.versions[versionID]
	if !ok || version.State != from {
		return ErrConflict
	}
	version.State = to
	repo.versions[versionID] = version
	return nil
}

func (repo *memoryArtifactRepository) FindDelivery(_ context.Context, scope Scope, idempotencyKey string) (Delivery, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	delivery, ok := repo.deliveries[deliveryMemoryKey(scope, idempotencyKey)]
	if !ok {
		return Delivery{}, ErrNotFound
	}
	delivery.ArtifactVersion = repo.versions[delivery.ArtifactVersion.VersionID]
	delivery.ArtifactVersion.Scope = scope
	delivery.Scope = scope
	return delivery, nil
}

func (repo *memoryArtifactRepository) CreateOutbound(_ context.Context, input CreateVersionInput) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	key := deliveryMemoryKey(input.Version.Scope, input.IdempotencyKey)
	if _, exists := repo.deliveries[key]; exists {
		return ErrConflict
	}
	repo.versions[input.Version.VersionID] = input.Version
	bindMemory(repo.bindings, input.Version.VersionID, input.Version.Scope.TurnID, 1)
	repo.deliveries[key] = Delivery{
		DeliveryID: input.DeliveryID, ArtifactVersion: input.Version, Scope: input.Version.Scope,
		IdempotencyKey: input.IdempotencyKey, BotTokenSecretRef: input.BotTokenSecretRef, State: input.DeliveryState,
	}
	return nil
}

func (repo *memoryArtifactRepository) SetDeliveryResult(_ context.Context, deliveryID string, state DeliveryState, fileID string, postID string, errorCode string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for key, delivery := range repo.deliveries {
		if delivery.DeliveryID != deliveryID || delivery.State == DeliveryDelivered || delivery.State == DeliveryQuarantined {
			continue
		}
		delivery.State = state
		if fileID != "" {
			delivery.MattermostFileID = fileID
		}
		if postID != "" {
			delivery.MattermostPostID = postID
		}
		delivery.ErrorCode = errorCode
		delivery.Attempts++
		repo.deliveries[key] = delivery
		return nil
	}
	return ErrConflict
}

func inboundMemoryKey(scope Scope, postID string, fileID string) string {
	return strings.Join([]string{scope.SessionKey, postID, fileID}, "/")
}

func deliveryMemoryKey(scope Scope, key string) string {
	return strings.Join([]string{scope.SessionKey, scope.TurnID, key}, "/")
}

func bindMemory(bindings map[string]map[string]int, versionID string, turnID string, ordinal int) {
	if bindings[versionID] == nil {
		bindings[versionID] = map[string]int{}
	}
	bindings[versionID][turnID] = ordinal
}
