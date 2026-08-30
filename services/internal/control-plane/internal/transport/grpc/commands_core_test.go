package grpc

import "testing"

func TestLaunchRunTitleSource(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, title, expected string
	}{
		{name: "название не передано", expected: "SERVER_DEFAULT"},
		{name: "название состоит из пробелов", title: " \t ", expected: "SERVER_DEFAULT"},
		{name: "название ввёл пользователь", title: "Проверить заявку", expected: "USER_EDITED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := launchRunTitleSource(test.title); actual != test.expected {
				t.Fatalf("launchRunTitleSource(%q) = %q, want %q", test.title, actual, test.expected)
			}
		})
	}
}
