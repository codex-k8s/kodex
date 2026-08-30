package integrationfixture

import "testing"

func TestConfiguredListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "default", want: defaultListenAddress},
		{name: "ephemeral loopback", value: "127.0.0.1:0", want: "127.0.0.1:0"},
		{name: "fixed loopback", value: "127.0.0.1:18083", want: "127.0.0.1:18083"},
		{name: "wildcard is forbidden", value: "0.0.0.0:18083", wantErr: true},
		{name: "privileged port is forbidden", value: "127.0.0.1:443", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(listenAddressEnv, test.value)
			got, err := configuredListenAddress()
			if test.wantErr {
				if err == nil {
					t.Fatalf("configuredListenAddress() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("configuredListenAddress() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("configuredListenAddress() = %q, want %q", got, test.want)
			}
		})
	}
}
