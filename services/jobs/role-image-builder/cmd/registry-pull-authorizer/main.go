package main

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/nodepullidentity"
)

const (
	// pullServerError is the fixed startup/serving diagnostic without identity-file details.
	// pullServerError — фиксированная диагностика запуска/обслуживания без подробностей файлов идентичности.
	pullServerError = "registry pull authorizer failed"
	// pullBasicAuthenticationChallenge is offered only for a missing profile credential.
	// pullBasicAuthenticationChallenge выдаётся только при отсутствии реквизитов профиля.
	pullBasicAuthenticationChallenge = `Basic realm="kodex-registry"`
	// internalPullRegistryHost is the only internal alias permitted beside the configured public registry.
	// internalPullRegistryHost — единственный внутренний псевдоним помимо настроенного внешнего реестра.
	internalPullRegistryHost = "kodex-image-registry.kodex-system.svc.cluster.local:5000"
	// pullRegistryHeaderTimeout bounds header acquisition separately from blob streaming.
	// pullRegistryHeaderTimeout ограничивает получение заголовков отдельно от передачи blob.
	pullRegistryHeaderTimeout = 5 * time.Second
	// pullRegistryStreamTimeout bounds both upstream and downstream active registry transfers.
	// pullRegistryStreamTimeout ограничивает активную передачу реестра в обоих направлениях.
	pullRegistryStreamTimeout = 15 * time.Minute
	// pullRegistryIdleTimeout closes inactive keep-alive connections without shortening active transfers.
	// pullRegistryIdleTimeout закрывает простаивающие keep-alive соединения, не сокращая активные передачи.
	pullRegistryIdleTimeout = 30 * time.Second
)

// pullProfile assigns a fixed Docker-config file and repository allowlist to
// one known certificate CN; clients cannot choose either through request fields.
// pullProfile назначает известному CN сертификата файл Docker-конфигурации и список
// репозиториев; клиент не выбирает их полями запроса.
type pullProfile struct {
	configFile   string
	repositories []string
}

// pullAuthorizationDecision represents either empty-field success or a closed
// failure/status/challenge; the challenge is not itself an authorization grant.
// pullAuthorizationDecision содержит пустые поля при успехе либо закрытый отказ,
// статус и запрос реквизитов; такой запрос сам по себе не выдаёт разрешение.
type pullAuthorizationDecision struct {
	failure       string
	statusCode    int
	authChallenge string
}

// main builds the fixed loopback pull proxy with both client CA roots, then
// authorizes each GET/HEAD before forwarding without its Authorization header.
// main создаёт фиксированный локальный pull-прокси с двумя клиентскими CA и
// разрешает каждый GET/HEAD перед передачей без заголовка Authorization.
func main() {
	registryHost := os.Getenv("REGISTRY_PULL_HOST")
	if !strings.Contains(registryHost, ".") || strings.ContainsAny(registryHost, "/:@?# \\\r\n\t") {
		log.Fatal(pullServerError)
	}
	pool := x509.NewCertPool()
	for _, path := range []string{"/identity/client-ca.pem", "/identity/node-client-ca.pem"} {
		value, err := os.ReadFile(path)
		if err != nil || !pool.AppendCertsFromPEM(value) {
			log.Fatal(pullServerError)
		}
	}
	backend, _ := url.Parse("http://127.0.0.1:5006")
	client := newPullRegistryProxyClient()
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		certificate, failure := pullClientCertificate(request)
		decision := denyPullRequest(failure)
		if failure == "" {
			decision = decidePullAuthorization(request, certificate, registryHost)
		}
		if decision.failure != "" {
			log.Printf("%s: request denied reason=%s", pullServerError, decision.failure)
			writePullAuthorizationDenial(writer, decision)
			return
		}
		target := *backend
		target.Path, target.RawQuery = request.URL.Path, request.URL.RawQuery
		upstream, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), nil)
		if err != nil {
			http.Error(writer, "upstream request rejected", http.StatusBadGateway)
			return
		}
		upstream.Header = request.Header.Clone()
		upstream.Header.Del("Authorization")
		response, err := client.Do(upstream)
		if err != nil {
			http.Error(writer, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				writer.Header().Add(key, value)
			}
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(writer, io.LimitReader(response.Body, 1<<30))
	})
	server := newPullRegistryProxyServer(":5000", handler, pool)
	if err := server.ListenAndServeTLS("/identity/tls.crt", "/identity/tls.key"); err != nil {
		log.Fatal(pullServerError)
	}
}

// newPullRegistryProxyClient bounds the entire upstream transfer and refuses
// redirects instead of allowing the registry response to select a different host.
// newPullRegistryProxyClient ограничивает всю передачу и отклоняет перенаправления,
// не позволяя ответу реестра выбрать другой хост.
func newPullRegistryProxyClient() *http.Client {
	return &http.Client{
		Timeout: pullRegistryStreamTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirect rejected")
		},
	}
}

// newPullRegistryProxyServer configures TLS 1.3 with mandatory client-chain
// verification and distinct header/stream/idle deadlines without starting the server.
// newPullRegistryProxyServer настраивает TLS 1.3 с обязательной проверкой клиентской
// цепочки и отдельными сроками заголовков/потока/простоя, не запуская сервер.
func newPullRegistryProxyServer(address string, handler http.Handler, pool *x509.CertPool) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: pullRegistryHeaderTimeout,
		ReadTimeout:       pullRegistryStreamTimeout,
		WriteTimeout:      pullRegistryStreamTimeout,
		IdleTimeout:       pullRegistryIdleTimeout,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  pool,
		},
	} //nolint:gosec // exact client CAs and TLS 1.3.
}

// pullClientCertificate selects the leaf from a nonempty TLS peer list and
// reports missing state/identity; it relies on the server's prior chain verification.
// pullClientCertificate выбирает первый сертификат непустой TLS-цепочки и сообщает
// отсутствие состояния/идентичности; проверка цепочки предполагается выполненной сервером.
func pullClientCertificate(request *http.Request) (*x509.Certificate, string) {
	if request == nil || request.TLS == nil {
		return nil, "tls_state"
	}
	if len(request.TLS.PeerCertificates) == 0 {
		return nil, "client_certificate"
	}
	// RequireAndVerifyClientCert уже проверил всю цепочку. Для профиля нужен
	// только leaf; наличие переданных intermediate не является ошибкой.
	return request.TLS.PeerCertificates[0], ""
}

// decidePullAuthorization first closes the method/path shape, then applies a
// known CN profile or node signature/IP binding and that identity's repository list.
// decidePullAuthorization сначала проверяет форму метода/пути, затем профиль известного
// CN либо подпись/IP узла и соответствующий идентичности список репозиториев.
func decidePullAuthorization(
	request *http.Request,
	certificate *x509.Certificate,
	registryHost string,
) pullAuthorizationDecision {
	if request.Method != http.MethodGet && request.Method != http.MethodHead || strings.ContainsAny(request.URL.Path, "\r\n\\") {
		return denyPullRequest("request_shape")
	}
	cn := certificate.Subject.CommonName
	profiles := pullProfiles()
	if profile, ok := profiles[cn]; ok {
		return decidePullProfileAuthorization(request, profile, registryHost)
	}
	if !authorizedNodePull(request, certificate, registryHost) {
		return denyPullRequest("node_identity")
	}
	if !pathInRepositories(request.URL.Path, nodePullRepositories()) {
		return denyPullRequest("node_repository")
	}
	return pullAuthorizationDecision{}
}

// pullProfiles returns fresh mappings for probe, base/input readers and installer;
// ordinary nodes use the separate certificate-bound credential path instead.
// pullProfiles возвращает новые соответствия для probe, чтения базы/входов и установщика;
// обычные узлы используют отдельную проверку реквизитов с привязкой к сертификату.
func pullProfiles() map[string]pullProfile {
	return map[string]pullProfile{
		"kodex-image-registry-pull-probe": {"/identity/probe-dockerconfig.json", []string{"kodex/control-plane"}},
		"kodex-buildkit-base-pull":        {"/identity/buildkit-dockerconfig.json", []string{"kodex/dockerfile", "kodex/agent-runner", "kodex/role-base-documents"}},
		"role-image-builder-input-read":   {"/identity/input-dockerconfig.json", []string{"kodex/role-image-inputs"}},
		"kodex-node-pull-installer":       {"/identity/pull-dockerconfigjson", nodePullRepositories()},
	}
}

// nodePullRepositories returns a fresh closed list of shipped platform images;
// probe, BuildKit and input-reader profiles retain their own narrower lists.
// nodePullRepositories возвращает новый закрытый список поставляемых образов платформы;
// профили probe, BuildKit и input-reader сохраняют собственные более узкие списки.
func nodePullRepositories() []string {
	return []string{
		"kodex/agent-runner", "kodex/roles", "kodex/session-archive", "kodex/role-image-builder",
		"kodex/control-plane", "kodex/secret-broker", "kodex/email-bridge", "kodex/runtime-controller",
		"kodex/integration-gateway", "kodex/integration-synthetic", "kodex/control-api-gateway",
		"kodex/staff-control-center", "kodex/egress-gateway", "kodex/automation-scheduler",
		"kodex/artifact-retention", "kodex/registry-pull-authorizer", "kodex/image-admission-controller",
		"kodex/image-admission", "kodex/image-admission-tools", "kodex/internal-rpc-authority",
		"kodex/stt-tts-service", "kodex/interaction-gateway", "kodex/backup-controller", "kodex/email-bridge-migration",
	}
}

// decidePullProfileAuthorization requires the fixed Docker-config credential
// before repository eligibility; only an absent header gets a 401 Basic challenge.
// decidePullProfileAuthorization требует реквизиты фиксированной Docker-конфигурации
// до проверки репозитория; только отсутствие заголовка вызывает Basic-запрос с кодом 401.
func decidePullProfileAuthorization(
	request *http.Request,
	profile pullProfile,
	registryHost string,
) pullAuthorizationDecision {
	username, password, supplied := request.BasicAuth()
	if !supplied {
		if len(request.Header.Values("Authorization")) == 0 {
			return pullAuthorizationDecision{
				failure:       "profile_credential_missing",
				statusCode:    http.StatusUnauthorized,
				authChallenge: pullBasicAuthenticationChallenge,
			}
		}
		return denyPullRequest("profile_credential")
	}
	if !dockerCredentialMatches(profile.configFile, username, password, registryHost) {
		return denyPullRequest("profile_credential")
	}
	if !pathInRepositories(request.URL.Path, profile.repositories) {
		return denyPullRequest("profile_repository")
	}
	return pullAuthorizationDecision{}
}

// denyPullRequest preserves empty success and maps every nonempty caller-owned
// failure code to Forbidden without an authentication challenge.
// denyPullRequest сохраняет пустой успех и превращает любой непустой код отказа
// вызывающей стороны в Forbidden без запроса аутентификации.
func denyPullRequest(failure string) pullAuthorizationDecision {
	if failure == "" {
		return pullAuthorizationDecision{}
	}
	return pullAuthorizationDecision{failure: failure, statusCode: http.StatusForbidden}
}

// writePullAuthorizationDenial emits only the decision's status/challenge and
// fixed denial text, not the internal failure reason or supplied credentials.
// writePullAuthorizationDenial выдаёт лишь статус/запрос реквизитов из решения
// и фиксированный отказ, не раскрывая внутреннюю причину и полученные реквизиты.
func writePullAuthorizationDenial(writer http.ResponseWriter, decision pullAuthorizationDecision) {
	if decision.authChallenge != "" {
		writer.Header().Set("WWW-Authenticate", decision.authChallenge)
	}
	http.Error(writer, "request denied", decision.statusCode)
}

// authorizedNodePull binds the remote IP to the certificate and verifies a
// v1 generation/CN-bound RSA-PSS credential over the selected registry host.
// Certificate trust/lifetime belongs to TLS verification, not this helper.
// authorizedNodePull связывает удалённый IP с сертификатом и проверяет RSA-PSS
// реквизиты v1 с поколением/CN и выбранным хостом реестра.
// Доверие и срок сертификата проверяет TLS, а не этот помощник.
func authorizedNodePull(request *http.Request, certificate *x509.Certificate, registryHost string) bool {
	remote, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || net.ParseIP(remote) == nil {
		return false
	}
	matchedIP := false
	for _, value := range certificate.IPAddresses {
		if value.Equal(net.ParseIP(remote)) {
			matchedIP = true
		}
	}
	if !matchedIP {
		return false
	}
	username, password, ok := request.BasicAuth()
	parts := strings.Split(password, ".")
	if !ok || username != certificate.Subject.CommonName || len(parts) != 3 || parts[0] != "v1" {
		return false
	}
	generation, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || !nodepullidentity.ValidCommonName(username, generation) {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if err != nil || !ok {
		return false
	}
	digest := sha256.Sum256([]byte(username + "\n" + parts[1] + "\n" + registryHost))
	return rsa.VerifyPSS(publicKey, cryptoHashSHA256, digest[:], signature, nil) == nil
}

// cryptoHashSHA256 fixes the RSA-PSS hash identifier to crypto.SHA256 rather
// than accepting an algorithm supplied through request data.
// cryptoHashSHA256 фиксирует идентификатор RSA-PSS хеша crypto.SHA256 вместо
// выбора алгоритма из данных запроса.
const cryptoHashSHA256 = 5 // crypto.SHA256; kept constant to avoid mutable algorithm selection.

// dockerCredentialMatches rejects configs over one MiB after reading and permits
// one/two known hosts, preferring the public host before comparing decoded credentials.
// It uses ordinary JSON decoding, not a duplicate-key-rejecting parser.
// dockerCredentialMatches после чтения отклоняет конфигурацию свыше одного МиБ
// и допускает один/два известных хоста, предпочитая внешний при сравнении реквизитов.
// Используется обычный JSON-разбор, а не декодер с запретом повторяющихся ключей.
func dockerCredentialMatches(path, username, password, registryHost string) bool {
	value, err := os.ReadFile(path)
	if err != nil || len(value) > 1<<20 {
		return false
	}
	var document struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if json.Unmarshal(value, &document) != nil || len(document.Auths) < 1 || len(document.Auths) > 2 {
		return false
	}
	for host := range document.Auths {
		if host != internalPullRegistryHost && host != registryHost {
			return false
		}
	}
	entry, ok := document.Auths[registryHost]
	if !ok {
		entry, ok = document.Auths[internalPullRegistryHost]
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	return ok && err == nil && string(decoded) == username+":"+password
}

// pathInRepositories accepts the registry root or manifests/blobs with a valid
// single selector in a listed repository; authentication remains the caller's responsibility.
// pathInRepositories допускает корень реестра либо manifests/blobs с корректным
// одиночным селектором заданного репозитория; аутентификацию обеспечивает вызывающий код.
func pathInRepositories(path string, repositories []string) bool {
	if path == "/v2/" {
		return true
	}
	for _, repository := range repositories {
		for _, operation := range []string{"manifests", "blobs"} {
			selector, ok := strings.CutPrefix(path, "/v2/"+repository+"/"+operation+"/")
			if ok && validPullSelector(selector) {
				return true
			}
		}
	}
	return false
}

// validPullSelector bounds a selector to 128 allowed ASCII characters and
// rejects empty/dot path components, slashes, escapes, queries and fragments.
// validPullSelector ограничивает селектор 128 допустимыми ASCII-символами и
// запрещает пустые/точечные компоненты, слеши, экранирование, запросы и фрагменты.
func validPullSelector(selector string) bool {
	if selector == "" || len(selector) > 128 || selector == "." || selector == ".." {
		return false
	}
	for _, character := range selector {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}
