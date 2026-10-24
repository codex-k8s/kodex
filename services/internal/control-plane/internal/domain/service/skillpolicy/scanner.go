package skillpolicy

import "context"

// ScanVerdict records scanner engine identity and infection outcome separately from structural
// skill validation.
// ScanVerdict хранит identity движка scanner и исход заражения отдельно от структурной проверки
// skill.
type ScanVerdict struct {
	Engine   string
	Infected bool
}

// Scanner defines context-bound content scanning as an external policy port; validating skill
// structure alone does not implement this scan.
// Scanner определяет проверку содержимого под context как внешний policy порт; сама структурная
// проверка skill не реализует этот scan.
type Scanner interface {
	// Scan returns engine and infection outcome for supplied skill bytes or a scan failure without
	// accepting a filesystem locator.
	// Scan возвращает движок и исход заражения переданных skill байтов либо отказ проверки без
	// принятия filesystem locator.
	Scan(context.Context, []byte) (ScanVerdict, error)
}
