package exactkubernetessecret

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const (
	// generationAnnotation records the map generation in metadata for comparison with the stored data marker.
	// generationAnnotation хранит поколение словаря в метаданных для сравнения с маркером в данных.
	generationAnnotation = "kodex.dev/secret-generation"
	// generationDataKey reserves the internal entry omitted from returned application snapshots.
	// generationDataKey резервирует внутреннюю запись, исключаемую из выдаваемых прикладных снимков.
	generationDataKey = "_generation"
)

// MapConfig binds the client to one preexisting Secret, a closed key set and a bounded HTTP timeout.
// MapConfig связывает клиент с одним существующим Secret, закрытым набором ключей и ограниченным HTTP-таймаутом.
type MapConfig struct {
	ResourceName    string
	AllowedDataKeys []string
	Timeout         time.Duration
}

// MapSnapshot carries the served version, generation and cloned application secret bytes without the internal marker; it is not a public API response.
// MapSnapshot содержит обслуженные версию, поколение и копии прикладных байтов секретов без внутреннего маркера; это не публичный API-ответ.
type MapSnapshot struct {
	ResourceVersion string
	Generation      uint64
	Data            map[string][]byte
}

// MapClient owns a fixed Secret and key allowlist and provides no list, create, delete or per-operation resource selection.
// MapClient владеет фиксированным Secret и списком ключей, не предоставляя list, create, delete и выбор ресурса в операции.
type MapClient struct {
	config  MapConfig
	allowed map[string]struct{}
	client  *http.Client
}

// NewMap validates resource, distinct application keys and timeout before loading trust; only the client adds the internal generation key.
// NewMap проверяет ресурс, уникальные прикладные ключи и таймаут до чтения доверия; только клиент добавляет внутренний ключ поколения.
func NewMap(config MapConfig) (*MapClient, error) {
	allowed := make(map[string]struct{}, len(config.AllowedDataKeys))
	for _, key := range config.AllowedDataKeys {
		if key == generationDataKey || !dataKey.MatchString(key) {
			return nil, errors.New("exact Kubernetes Secret map key is invalid")
		}
		allowed[key] = struct{}{}
	}
	allowed[generationDataKey] = struct{}{}
	if !validResourceName(config.ResourceName) || len(allowed) == 0 ||
		len(allowed) > 33 || len(allowed) != len(config.AllowedDataKeys)+1 ||
		config.Timeout < time.Second || config.Timeout > 10*time.Second {
		return nil, errors.New("exact Kubernetes Secret map configuration is invalid")
	}
	httpClient, err := newKubernetesHTTPClient(config.Timeout)
	if err != nil {
		return nil, err
	}
	return &MapClient{config: config, allowed: allowed, client: httpClient}, nil
}

// Read validates the fixed Secret and matching generation markers, returning copied application entries; generation zero is accepted only for empty bootstrap data.
// Read проверяет фиксированный Secret и совпадение маркеров поколения, возвращая копии прикладных записей; нулевое поколение допустимо лишь для пустых начальных данных.
func (client *MapClient) Read(ctx context.Context) (MapSnapshot, error) {
	secret, generation, err := client.read(ctx)
	if err != nil {
		return MapSnapshot{}, err
	}
	return MapSnapshot{
		ResourceVersion: secret.Metadata.ResourceVersion,
		Generation:      generation,
		Data:            publicMapData(secret.Data),
	}, nil
}

// CompareAndSwap requires expected version and generation, replaces the allowed data with incremented markers and verifies exact served readback after PUT.
// CompareAndSwap требует ожидаемые версию и поколение, заменяет допустимые данные с увеличением маркеров и после PUT проверяет точный обслуженный снимок.
func (client *MapClient) CompareAndSwap(
	ctx context.Context,
	expectedResourceVersion string,
	expectedGeneration uint64,
	data map[string][]byte,
) (MapSnapshot, error) {
	if expectedResourceVersion == "" || data[generationDataKey] != nil || !client.validData(data) {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map CAS input is invalid")
	}
	secret, generation, err := client.read(ctx)
	if err != nil {
		return MapSnapshot{}, err
	}
	if secret.Metadata.ResourceVersion != expectedResourceVersion || generation != expectedGeneration {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map CAS rejected")
	}
	annotations := map[string]string{}
	if raw := secret.Metadata.raw["annotations"]; len(raw) > 0 {
		if json.Unmarshal(raw, &annotations) != nil {
			return MapSnapshot{}, errors.New("exact Kubernetes Secret annotations are invalid")
		}
	}
	annotations[generationAnnotation] = strconv.FormatUint(generation+1, 10)
	rawAnnotations, err := json.Marshal(annotations)
	if err != nil {
		return MapSnapshot{}, errors.New("encode exact Kubernetes Secret annotations")
	}
	secret.Metadata.raw["annotations"] = rawAnnotations
	storedData := cloneData(data)
	storedData[generationDataKey] = []byte(strconv.FormatUint(generation+1, 10))
	secret.Data = storedData
	body, err := json.Marshal(secret)
	if err != nil {
		return MapSnapshot{}, errors.New("encode exact Kubernetes Secret map update")
	}
	request, err := client.request(ctx, http.MethodPut, bytes.NewReader(body))
	if err != nil {
		return MapSnapshot{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return MapSnapshot{}, errors.New("update exact Kubernetes Secret map")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
	if response.StatusCode == http.StatusConflict {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map CAS rejected")
	}
	if response.StatusCode != http.StatusOK {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map update rejected")
	}
	served, err := client.Read(ctx)
	if err != nil || served.ResourceVersion == expectedResourceVersion ||
		served.Generation != expectedGeneration+1 || !equalData(served.Data, data) {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map served readback mismatch")
	}
	return served, nil
}

// Check runs the full map read and generation validation without returning secret contents to the readiness caller.
// Check выполняет полное чтение словаря и проверку поколения, не возвращая содержимое секретов проверяющему готовность.
func (client *MapClient) Check(ctx context.Context) error {
	_, err := client.Read(ctx)
	return err
}

// Close releases idle HTTP connections while leaving in-flight cancellation to request contexts.
// Close освобождает простаивающие HTTP-соединения, оставляя отмену выполняющихся запросов их контекстам.
func (client *MapClient) Close() { client.client.CloseIdleConnections() }

// read checks the bounded exact mutable Opaque Secret, matching annotation and data generations and the closed key set before exposing contents.
// read проверяет ограниченный точный изменяемый Opaque Secret, совпадение поколений аннотации и данных и закрытый набор ключей до раскрытия содержимого.
func (client *MapClient) read(ctx context.Context) (secretDocument, uint64, error) {
	request, err := client.request(ctx, http.MethodGet, nil)
	if err != nil {
		return secretDocument{}, 0, err
	}
	response, err := client.client.Do(request)
	if err != nil {
		return secretDocument{}, 0, errors.New("read exact Kubernetes Secret map")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map read rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumResponseBytes {
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map response is invalid")
	}
	var secret secretDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&secret) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		secret.APIVersion != "v1" || secret.Kind != "Secret" ||
		secret.Metadata.Name != client.config.ResourceName ||
		secret.Metadata.Namespace != KubernetesNamespace || secret.Metadata.ResourceVersion == "" ||
		secret.Type != "Opaque" || (secret.Immutable != nil && *secret.Immutable) {
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map binding is invalid")
	}
	generation, err := mapGeneration(secret.Metadata.raw["annotations"])
	servedGeneration, servedGenerationErr := dataGeneration(secret.Data)
	if err != nil || servedGenerationErr != nil || generation != servedGeneration ||
		(generation == 0 && len(secret.Data) != 0) ||
		(generation > 0 && !client.validData(secret.Data)) {
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map generation is invalid")
	}
	return secret, generation, nil
}

// request refreshes the projected token for each call and builds only the fixed Secret endpoint, preserving the map client's bounded construction diagnostic.
// request обновляет проецируемый токен каждым вызовом и строит лишь фиксированный адрес Secret, сохраняя ограниченную диагностику построения клиента словаря.
func (client *MapClient) request(ctx context.Context, method string, body io.Reader) (*http.Request, error) {
	token, err := readKubernetesAPIToken()
	if err != nil {
		return nil, err
	}
	request, err := newKubernetesSecretRequest(ctx, method, client.config.ResourceName, token, body)
	if err != nil {
		return nil, errors.New("construct exact Kubernetes Secret map request")
	}
	return request, nil
}

// validData accepts a nonempty subset of registered keys with nonempty bounded values, including the internal marker only when registered in the client's set.
// validData принимает непустое подмножество зарегистрированных ключей с непустыми ограниченными значениями, включая внутренний маркер лишь при регистрации в наборе клиента.
func (client *MapClient) validData(values map[string][]byte) bool {
	if len(values) == 0 || len(values) > len(client.allowed) {
		return false
	}
	for key, value := range values {
		if _, ok := client.allowed[key]; !ok || len(value) == 0 || len(value) > 1<<20 {
			return false
		}
	}
	return true
}

// mapGeneration reads the decimal generation annotation, treating absent annotations or key as bootstrap zero and rejecting malformed maps or numbers.
// mapGeneration читает десятичную аннотацию поколения, трактуя отсутствие аннотаций или ключа как начальный ноль и отклоняя некорректные словари и числа.
func mapGeneration(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var annotations map[string]string
	if json.Unmarshal(raw, &annotations) != nil {
		return 0, errors.New("decode exact Kubernetes Secret annotations")
	}
	value, ok := annotations[generationAnnotation]
	if !ok {
		return 0, nil
	}
	generation, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New("parse exact Kubernetes Secret generation")
	}
	return generation, nil
}

// dataGeneration accepts zero only for empty bootstrap data and otherwise requires a positive decimal internal marker.
// dataGeneration принимает ноль лишь для пустых начальных данных, в остальных случаях требуя положительного десятичного внутреннего маркера.
func dataGeneration(data map[string][]byte) (uint64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	raw, ok := data[generationDataKey]
	if !ok {
		return 0, errors.New("exact Kubernetes Secret data generation is absent")
	}
	generation, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil || generation == 0 {
		return 0, errors.New("parse exact Kubernetes Secret data generation")
	}
	return generation, nil
}

// cloneData copies the map and each byte slice so returned snapshots cannot mutate stored secret data.
// cloneData копирует словарь и каждый байтовый срез, чтобы выданные снимки не меняли хранимые секретные данные.
func cloneData(values map[string][]byte) map[string][]byte {
	clone := make(map[string][]byte, len(values))
	for key, value := range values {
		clone[key] = bytes.Clone(value)
	}
	return clone
}

// publicMapData removes the internal generation marker from an independent map copy; remaining application values are still secret bytes.
// publicMapData удаляет внутренний маркер поколения из независимой копии словаря; оставшиеся прикладные значения по-прежнему являются секретными байтами.
func publicMapData(values map[string][]byte) map[string][]byte {
	result := cloneData(values)
	delete(result, generationDataKey)
	return result
}

// equalData compares map cardinality and bytes in deterministic key order without modifying either input.
// equalData сравнивает количество записей и байты в устойчивом порядке ключей без изменения входов.
func equalData(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	keys := make([]string, 0, len(left))
	for key := range left {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !bytes.Equal(left[key], right[key]) {
			return false
		}
	}
	return true
}
