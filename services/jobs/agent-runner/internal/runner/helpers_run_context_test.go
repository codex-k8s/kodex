package runner

import (
	"reflect"
	"testing"
)

func TestGitCleanArgs(t *testing.T) {
	t.Run("full-env preserves ignored runtime artifacts", func(t *testing.T) {
		if got, want := gitCleanArgs(runtimeModeFullEnv), []string{"clean", "-fd"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("gitCleanArgs(full-env) = %v, want %v", got, want)
		}
	})

	t.Run("code-only keeps hard cleanup", func(t *testing.T) {
		if got, want := gitCleanArgs(runtimeModeCodeOnly), []string{"clean", "-fdx"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("gitCleanArgs(code-only) = %v, want %v", got, want)
		}
	})

	t.Run("unknown mode defaults to code-only cleanup", func(t *testing.T) {
		if got, want := gitCleanArgs("unexpected"), []string{"clean", "-fdx"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("gitCleanArgs(unexpected) = %v, want %v", got, want)
		}
	})
}
