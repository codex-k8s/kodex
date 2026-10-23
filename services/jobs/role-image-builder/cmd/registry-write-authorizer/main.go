package main

import (
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/httpserver/streamproxy"
)

const (
	// expectedClientCN selects the staging push certificate identity, not a repository grant by itself.
	// expectedClientCN выбирает сертификат отправителя staging, сам по себе не выдавая доступ к репозиторию.
	expectedClientCN = "kodex-buildkit-staging-push"
	// evidenceBasicAuthenticationChallenge invites only a missing application credential to retry Basic authentication.
	// evidenceBasicAuthenticationChallenge предлагает Basic-аутентификацию только при отсутствии прикладных реквизитов.
	evidenceBasicAuthenticationChallenge = `Basic realm="kodex-image-registry-evidence"`
	// serverError is the fixed startup/serving failure that avoids exposing identity-file or TLS details.
	// serverError — фиксированная ошибка запуска/обслуживания без подробностей файлов идентичности и TLS.
	serverError = "registry write authorizer failed"
	// denialLog is the shared denial prefix; only closed reasons and normalized methods are appended.
	// denialLog — общий префикс отказа, дополняемый только закрытой причиной и нормализованным методом.
	denialLog = "registry request denied"
	// registryHeaderTimeout limits request-header acquisition independently from long blob transfers.
	// registryHeaderTimeout ограничивает получение заголовков отдельно от длительной передачи blob.
	registryHeaderTimeout = 5 * time.Second
	// registryStreamTimeout bounds client and server transfer lifetimes for large registry payloads.
	// registryStreamTimeout ограничивает передачу крупных данных реестра на клиенте и сервере.
	registryStreamTimeout = 15 * time.Minute
	// registryIdleTimeout closes idle HTTP connections without reducing the active stream budget.
	// registryIdleTimeout закрывает простаивающие HTTP-соединения, не сокращая бюджет активного потока.
	registryIdleTimeout = 30 * time.Second
)

// authorizationProfile chooses the fixed staging/evidence listener and loopback
// backend; the evidence flag selects a separate certificate/credential policy.
// authorizationProfile выбирает фиксированные staging/evidence адреса и локальный
// сервер; флаг evidence включает отдельную политику сертификатов и реквизитов.
type authorizationProfile struct {
	address, backend string
	evidence         bool
}

// main serves the selected mTLS registry proxy, checking operation scope before
// forwarding and stripping application Authorization from the loopback request.
// Evidence additionally checks its identity-specific Basic credential; staging uses its fixed CN.
// main обслуживает выбранный mTLS-прокси реестра, проверяя область операции до
// передачи и удаляя прикладной Authorization из запроса локальному серверу.
// Evidence дополнительно проверяет Basic-реквизиты идентичности, staging использует заданный CN.
func main() {
	ca, err := os.ReadFile("/identity/client-ca.pem")
	if err != nil {
		log.Fatal(serverError)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		log.Fatal(serverError)
	}
	profile := authorizationProfile{address: ":5001", backend: "http://127.0.0.1:5005"}
	if os.Getenv("REGISTRY_AUTHORIZATION_PROFILE") == "evidence" {
		profile = authorizationProfile{address: ":5007", backend: "http://127.0.0.1:5008", evidence: true}
	} else if value := os.Getenv("REGISTRY_AUTHORIZATION_PROFILE"); value != "" && value != "staging" {
		log.Fatal(serverError)
	}
	backend, _ := url.Parse(profile.backend)
	client := streamproxy.NewClient(registryStreamTimeout)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 ||
			len(request.TLS.VerifiedChains) == 0 {
			logRegistryDenial(profile, "unverified_mtls", request.Method)
			http.Error(writer, "request forbidden", http.StatusForbidden)
			return
		}
		commonName := request.TLS.PeerCertificates[0].Subject.CommonName
		if !profile.evidence &&
			(commonName != expectedClientCN || !allowedRegistryWrite(request.Method, request.URL.Path)) {
			logRegistryDenial(profile, "operation", request.Method)
			http.Error(writer, "request forbidden", http.StatusForbidden)
			return
		}
		if profile.evidence && !allowedEvidenceRequest(commonName, request.Method, request.URL.Path) {
			logRegistryDenial(profile, "operation", request.Method)
			http.Error(writer, "request forbidden", http.StatusForbidden)
			return
		}
		if profile.evidence &&
			!authorizedEvidenceCredential(commonName, request.Header.Get("Authorization")) {
			logRegistryDenial(profile, "application_credential", request.Method)
			writeEvidenceCredentialDenial(writer, request.Header.Values("Authorization"))
			return
		}
		streamproxy.Forward(writer, request, backend, request.Body, client, 1<<30)
	})
	server := streamproxy.NewMTLSServer(profile.address, handler, pool, streamproxy.Timeouts{
		Header: registryHeaderTimeout, Stream: registryStreamTimeout, Idle: registryIdleTimeout,
	})
	if err := server.ListenAndServeTLS("/identity/tls.crt", "/identity/tls.key"); err != nil {
		log.Fatal(serverError)
	}
}

// writeEvidenceCredentialDenial returns a Basic challenge only when no header
// was supplied; malformed or wrong supplied credentials remain Forbidden.
// writeEvidenceCredentialDenial выдаёт Basic-запрос только при отсутствии заголовка;
// переданные неверные или повреждённые реквизиты остаются Forbidden.
func writeEvidenceCredentialDenial(writer http.ResponseWriter, authorization []string) {
	status := http.StatusForbidden
	if len(authorization) == 0 {
		writer.Header().Set("WWW-Authenticate", evidenceBasicAuthenticationChallenge)
		status = http.StatusUnauthorized
	}
	http.Error(writer, "request forbidden", status)
}

// logRegistryDenial emits the selected closed profile/reason and normalized method,
// without recording request paths, credentials, payloads or certificate contents.
// logRegistryDenial пишет выбранные закрытые профиль/причину и нормализованный метод,
// не записывая пути запросов, реквизиты, содержимое и данные сертификата.
func logRegistryDenial(profile authorizationProfile, reason, method string) {
	profileName := "staging"
	if profile.evidence {
		profileName = "evidence"
	}
	log.Printf(
		"%s: profile=%s reason=%s method=%s",
		denialLog,
		profileName,
		reason,
		normalizeMethod(method),
	)
}

// normalizeMethod preserves the six known HTTP methods and maps every other
// string to OTHER, bounding the diagnostic method vocabulary.
// normalizeMethod сохраняет шесть известных HTTP-методов, заменяя любую другую
// строку на OTHER для ограничения множества диагностических значений.
func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch,
		http.MethodPut, http.MethodDelete:
		return method
	default:
		return "OTHER"
	}
}

// allowedEvidenceRequest separates admission writes, promotion reads and probe-only
// root checks for the evidence repository; deletion and other identities are denied.
// allowedEvidenceRequest разделяет запись допуска, чтение продвижения и проверку корня
// для probe в evidence-репозитории; удаление и прочие идентичности запрещены.
func allowedEvidenceRequest(commonName, method, path string) bool {
	if path == "/v2/" && method == http.MethodGet {
		return commonName == "image-admission" || commonName == "image-promotion" ||
			commonName == "kodex-image-registry-evidence-probe"
	}
	if commonName == "kodex-image-registry-evidence-probe" || method == http.MethodDelete ||
		!pathInRegistryRepository(path, "evidence/role-image-admission") || strings.ContainsAny(path, "\r\n\\") {
		return false
	}
	if commonName == "image-promotion" {
		return method == http.MethodGet || method == http.MethodHead
	}
	if commonName != "image-admission" {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch, http.MethodPut:
		return true
	default:
		return false
	}
}

// authorizedEvidenceCredential reads the fixed identity-specific username/password
// files for each check; it grants no operation beyond the separate repository policy.
// authorizedEvidenceCredential при каждой проверке читает фиксированные файлы имени/пароля
// выбранной идентичности и не расширяет отдельную политику операций репозитория.
func authorizedEvidenceCredential(commonName, authorization string) bool {
	return authorizedEvidenceCredentialWithReader(commonName, authorization, os.ReadFile)
}

// authorizedEvidenceCredentialWithReader selects known identity files, rejects
// empty/over-4-KiB values after reading, trims terminal CR/LF and compares canonical Basic.
// Reader injection supports synthetic tests and does not accept caller-selected paths.
// authorizedEvidenceCredentialWithReader выбирает файлы известных идентичностей и после чтения
// отклоняет пустые/свыше 4 КиБ значения, убирает конечные CR/LF и сравнивает канонический Basic.
// Подмена reader нужна тестам и не разрешает путь из запроса клиента.
func authorizedEvidenceCredentialWithReader(
	commonName string,
	authorization string,
	readFile func(string) ([]byte, error),
) bool {
	prefix := "/identity/"
	switch commonName {
	case "image-admission":
		prefix += "admission"
	case "image-promotion":
		prefix += "promotion"
	case "kodex-image-registry-evidence-probe":
		prefix += "probe"
	default:
		return false
	}
	username, userErr := readFile(prefix + ".username")
	password, passwordErr := readFile(prefix + ".password")
	if userErr != nil || passwordErr != nil || len(username) == 0 || len(password) == 0 ||
		len(username) > 4096 || len(password) > 4096 {
		return false
	}
	usernameValue := strings.TrimRight(string(username), "\r\n")
	passwordValue := strings.TrimRight(string(password), "\r\n")
	if usernameValue == "" || passwordValue == "" || strings.ContainsAny(usernameValue, "\r\n") ||
		strings.ContainsAny(passwordValue, "\r\n") {
		return false
	}
	credential := usernameValue + ":" + passwordValue
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
	return subtle.ConstantTimeCompare([]byte(authorization), []byte(expected)) == 1
}

// allowedRegistryWrite permits the known read/upload verbs only under staging
// image/readiness operation prefixes, plus GET /v2/; deletes and other roots are denied.
// allowedRegistryWrite разрешает известные методы чтения/загрузки только в префиксах
// staging image/readiness и GET /v2/; удаление и другие корни запрещены.
func allowedRegistryWrite(method, path string) bool {
	if method == http.MethodGet && path == "/v2/" {
		return true
	}
	if method == http.MethodDelete || !strings.HasPrefix(path, "/v2/") || strings.ContainsAny(path, "\r\n\\") {
		return false
	}
	for _, candidate := range []string{"staging/role-images", "staging/readiness"} {
		if pathInRegistryRepository(path, candidate) {
			switch method {
			case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch, http.MethodPut:
				return true
			default:
				return false
			}
		}
	}
	return false
}

// pathInRegistryRepository recognizes blobs/manifests prefixes under an exact
// repository prefix; it does not independently validate the remaining selector syntax.
// pathInRegistryRepository распознаёт префиксы blobs/manifests заданного репозитория;
// синтаксис оставшегося селектора отдельно не проверяется.
func pathInRegistryRepository(path, repository string) bool {
	remainder := strings.TrimPrefix(path, "/v2/"+repository+"/")
	return remainder != path && (strings.HasPrefix(remainder, "blobs/") || strings.HasPrefix(remainder, "manifests/"))
}
