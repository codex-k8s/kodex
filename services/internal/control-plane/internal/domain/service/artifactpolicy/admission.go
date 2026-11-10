package artifactpolicy

// ScanNotScanned is an honest ordinary-file admission state under an explicit deployment policy.
// It records absence of antivirus inspection, never an equivalent CLEAN or executable admission.
// ScanNotScanned — честное состояние обычного файла при явной политике установки: антивирусная
// проверка отсутствует. Оно не равнозначно CLEAN и не допускает исполняемое содержимое.
const ScanNotScanned = "NOT_SCANNED"

// ContentAdmitted recognizes the two ordinary-content states usable after owner receipt checks.
// It grants no authority, proves no digest and must not replace required AV checks for skills,
// avatars or repository-bundle publication. Unknown states stay closed.
// ContentAdmitted распознаёт два состояния обычного содержимого после проверки owner receipt.
// Не выдаёт права, не доказывает хеш и не заменяет обязательный AV для skills, avatars и публикации
// repository bundles. Неизвестные состояния закрыто отклоняются.
func ContentAdmitted(state string) bool {
	return state == ScanClean || state == ScanNotScanned
}
