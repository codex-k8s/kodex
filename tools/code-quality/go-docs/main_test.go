package main

import "testing"

// TestInspectFileRecognizesBilingualOrder rejects Russian-only and reversed comments.
// TestInspectFileRecognizesBilingualOrder отклоняет только русский и обратный порядок языков.
func TestInspectFileRecognizesBilingualOrder(t *testing.T) {
	declarations, err := inspectFile("example.go", []byte(`package example
// Value preserves the original payload without mutation.
// Value сохраняет исходные данные без изменения.
type Value string
// Read выполняет чтение.
func Read() {}
// Write сохраняет значение.
// Write stores a value.
func Write() {}
type Store interface {
    // Load returns the current value without changing it.
    // Load возвращает текущее значение без изменения.
    Load() Value
}
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations) != 5 {
		t.Fatalf("expected five declarations, got %d", len(declarations))
	}
	for i, expected := range []bool{true, false, false, false, true} {
		if declarations[i].Bilingual != expected {
			t.Fatalf("declaration %s: expected bilingual=%t", declarations[i].Name, expected)
		}
	}
}

// TestInspectFileRejectsSyntaxErrors prevents partial parse recovery from hiding declarations.
// TestInspectFileRejectsSyntaxErrors запрещает скрывать объявления после ошибки разбора.
func TestInspectFileRejectsSyntaxErrors(t *testing.T) {
	if _, err := inspectFile("broken.go", []byte("package example\nfunc Broken(")); err == nil {
		t.Fatal("expected syntax error")
	}
}

// TestInspectFileRejectsIdentifierOnly requires English prose rather than a bare
// symbol name followed by a Russian sentence.
// TestInspectFileRejectsIdentifierOnly требует английское пояснение, а не только
// имя символа перед русским предложением.
func TestInspectFileRejectsIdentifierOnly(t *testing.T) {
	declarations, err := inspectFile("example.go", []byte("package example\n// Value\n// Значение для проверки.\ntype Value string"))
	if err != nil || len(declarations) != 1 || declarations[0].Bilingual {
		t.Fatal("identifier-only comment passed the bilingual format check")
	}
}

// TestInspectFileIncludesLocalWireAlias prevents function-local named types from
// disappearing from documentation coverage while ignoring ordinary local values.
// TestInspectFileIncludesLocalWireAlias не даёт скрыть локальные именованные типы
// из покрытия комментариями, не считая обычные локальные значения сущностями.
func TestInspectFileIncludesLocalWireAlias(t *testing.T) {
	declarations, err := inspectFile("example.go", []byte("package example\nfunc Decode(){\ntype wire string\nvar local wire\n_ = local\n}"))
	if err != nil || len(declarations) != 2 || declarations[1].Name != "wire" || declarations[1].Bilingual {
		t.Fatal("local wire alias was lost or incorrectly documented")
	}
}
