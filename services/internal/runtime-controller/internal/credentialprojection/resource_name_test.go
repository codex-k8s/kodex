package credentialprojection

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestProjectionDNSLabelKeepsLetterFirst preserves the client's pre-existing stricter first-letter
// rule while the shared grammar permits numeric-leading labels; length, controls and separator limits
// remain closed, and this unit neither reads a Secret nor calls the broker.
// TestProjectionDNSLabelKeepsLetterFirst сохраняет прежнее более строгое правило первой буквы
// клиента, хотя общая грамматика допускает первую цифру; ограничения длины, управляющих символов
// и разделителей сохраняются, unit не читает Secret и не вызывает брокер.
func TestProjectionDNSLabelKeepsLetterFirst(t *testing.T) {
	for _, value := range []string{"0", "7projection", "9-a"} {
		if !runtimecontract.ValidDNSLabel(value) || validDNSLabel(value) {
			t.Fatal("projection client lost its existing first-letter restriction")
		}
	}
	for _, value := range []string{"a", "projection-7", "a--9", strings.Repeat("a", 63)} {
		if !validDNSLabel(value) {
			t.Fatal("valid letter-first projection name rejected")
		}
	}
	for _, value := range []string{"", "-a", "a-", "A", "а", "a.b", "a\x00b", "a\n", strings.Repeat("a", 64)} {
		if validDNSLabel(value) {
			t.Fatal("invalid projection name accepted")
		}
	}
}
