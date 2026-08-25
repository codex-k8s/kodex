package exactkubernetessecret

import (
	"encoding/json"
	"testing"
)

func TestMapGeneration(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"kodex.dev/secret-generation":"7"}`)
	generation, err := mapGeneration(raw)
	if err != nil || generation != 7 {
		t.Fatalf("mapGeneration() = %d, %v; want 7, nil", generation, err)
	}
	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"kodex.dev/secret-generation":"0"}`),
		json.RawMessage(`{"kodex.dev/secret-generation":"invalid"}`),
		json.RawMessage(`[]`),
	} {
		if _, err := mapGeneration(invalid); err == nil {
			t.Fatalf("mapGeneration() accepted %s", invalid)
		}
	}
}

func TestMapClientValidDataUsesExactKeys(t *testing.T) {
	t.Parallel()

	client := &MapClient{allowed: map[string]struct{}{"private.jwk": {}, "bundle.jws": {}}}
	if !client.validData(map[string][]byte{
		"private.jwk": []byte("private"),
		"bundle.jws":  []byte("bundle"),
	}) {
		t.Fatal("exact data set was rejected")
	}
	if !client.validData(map[string][]byte{"private.jwk": []byte("private")}) {
		t.Fatal("allowed optional data subset was rejected")
	}
	for _, invalid := range []map[string][]byte{
		{},
		{"private.jwk": []byte("private"), "unknown": []byte("value")},
		{"private.jwk": nil, "bundle.jws": []byte("bundle")},
	} {
		if client.validData(invalid) {
			t.Fatalf("invalid data set was accepted: %#v", invalid)
		}
	}
}
