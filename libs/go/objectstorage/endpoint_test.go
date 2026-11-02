package objectstorage

import "testing"

// TestValidEndpoint checks URL shape, explicit plaintext origin matching and preservation of accepted HTTPS syntax without network access.
// RU: TestValidEndpoint проверяет форму URL, точное совпадение явно разрешённого открытого адреса и сохранение допустимого синтаксиса HTTPS без сетевых обращений.
func TestValidEndpoint(t *testing.T) {
	t.Parallel()
	policy := EndpointPolicy{AllowPlaintext: true, PlaintextHostname: "storage.example.test", PlaintextPort: "8333"}
	for _, test := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "https", url: "https://s3.example.test", want: true},
		{name: "https root", url: "https://s3.example.test/", want: true},
		{name: "https explicit port", url: "https://s3.example.test:9443", want: true},
		{name: "https IPv6", url: "https://[2001:db8::1]:443/", want: true},
		{name: "empty query delimiter", url: "https://s3.example.test/?", want: true},
		{name: "encoded root", url: "https://s3.example.test/%2e"},
		{name: "escaped root slash", url: "https://s3.example.test/%2f"},
		{name: "encoded root without extra slash", url: "https://s3.example.test%2f"},
		{name: "exact plaintext origin", url: "http://storage.example.test:8333/", want: true},
		{name: "different plaintext host", url: "http://other.example.test:8333/"},
		{name: "different plaintext host case", url: "http://STORAGE.example.test:8333/"},
		{name: "different plaintext port", url: "http://storage.example.test:9000/"},
		{name: "missing plaintext port", url: "http://storage.example.test/"},
		{name: "empty URL"},
		{name: "relative URL", url: "storage.example.test"},
		{name: "missing host", url: "https:///"},
		{name: "opaque URL", url: "https:storage.example.test"},
		{name: "unsupported scheme", url: "ftp://storage.example.test:8333"},
		{name: "invalid numeric port", url: "https://s3.example.test:invalid"},
		{name: "invalid escape", url: "https://s3.example.test/%zz"},
		{name: "userinfo", url: "https://user:password@s3.example.test"},
		{name: "empty userinfo", url: "https://@s3.example.test"},
		{name: "path", url: "https://s3.example.test/bucket"},
		{name: "query", url: "https://s3.example.test/?key=value"},
		{name: "fragment", url: "https://s3.example.test/#fragment"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidEndpoint(test.url, policy); got != test.want {
				t.Fatalf("ValidEndpoint = %v, want %v", got, test.want)
			}
		})
	}
}

// TestValidEndpointRequiresCompletePlaintextOptIn rejects partial plaintext policy while HTTPS remains independent of the exception.
// RU: TestValidEndpointRequiresCompletePlaintextOptIn отклоняет неполную политику открытого транспорта, сохраняя независимость HTTPS от исключения.
func TestValidEndpointRequiresCompletePlaintextOptIn(t *testing.T) {
	t.Parallel()
	for _, policy := range []EndpointPolicy{
		{},
		{PlaintextHostname: "storage.example.test", PlaintextPort: "8333"},
		{AllowPlaintext: true, PlaintextPort: "8333"},
		{AllowPlaintext: true, PlaintextHostname: "storage.example.test"},
	} {
		if ValidEndpoint("http://storage.example.test:8333/", policy) {
			t.Fatal("incomplete plaintext policy accepted HTTP")
		}
		if !ValidEndpoint("https://s3.example.test/", policy) {
			t.Fatal("plaintext policy unexpectedly restricted HTTPS")
		}
	}
}

// TestHTTPSMountPrefixRequiresPathStyle accepts only a canonical TLS mount chosen by deployment configuration.
// RU: TestHTTPSMountPrefixRequiresPathStyle допускает лишь канонический TLS mount из конфигурации установки.
func TestHTTPSMountPrefixRequiresPathStyle(t *testing.T) {
	t.Parallel()
	for _, value := range []struct {
		name, endpoint  string
		pathStyle, want bool
	}{
		{"single segment", "https://s3.example.invalid/s3", true, true},
		{"nested mount", "https://s3.example.invalid/services/object-store", true, true},
		{"root remains valid", "https://s3.example.invalid/", false, true},
		{"explicit path style required", "https://s3.example.invalid/s3", false, false},
		{"plaintext mount forbidden", "http://storage.example.invalid:8333/s3", true, false},
		{"encoded mount", "https://s3.example.invalid/%73%33", true, false},
		{"dot traversal", "https://s3.example.invalid/s3/../api", true, false},
		{"encoded traversal", "https://s3.example.invalid/s3/%2e%2e/api", true, false},
		{"duplicate slash", "https://s3.example.invalid/s3//api", true, false},
		{"trailing slash", "https://s3.example.invalid/s3/", true, false},
		{"query", "https://s3.example.invalid/s3?token=synthetic", true, false},
		{"fragment", "https://s3.example.invalid/s3#fragment", true, false},
	} {
		t.Run(value.name, func(t *testing.T) {
			policy := EndpointPolicy{UsePathStyle: value.pathStyle, AllowPlaintext: true, PlaintextHostname: "storage.example.invalid", PlaintextPort: "8333"}
			if got := ValidEndpoint(value.endpoint, policy); got != value.want {
				t.Fatalf("endpoint accepted=%v, want %v", got, value.want)
			}
		})
	}
}
