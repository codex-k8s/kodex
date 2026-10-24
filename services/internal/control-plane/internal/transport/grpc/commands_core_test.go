package grpc

import "testing"

// TestLaunchRunTitleSource checks empty or whitespace-only run titles use SERVER_DEFAULT while a
// supplied user title uses USER_EDITED.
// TestLaunchRunTitleSource проверяет SERVER_DEFAULT для пустых и пробельных заголовков запуска и
// USER_EDITED для переданного пользовательского заголовка.
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
