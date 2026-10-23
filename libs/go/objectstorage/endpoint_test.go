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
