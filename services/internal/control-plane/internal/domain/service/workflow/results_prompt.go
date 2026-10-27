package workflow

import (
	"encoding/json"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ResultDeclarationPrompt renders the closed final-result shape for published result keys.
// It supplies no evidence or permissions: external selectors are deliberately invalid placeholders,
// while CURRENT_COMPLETION still requires owner-verified files from the current completion.
// ResultDeclarationPrompt показывает закрытую форму финального ответа для опубликованных ключей.
// Подсказка не выдаёт доказательств или полномочий: внешние ссылки намеренно заменены невалидными
// обозначениями, а CURRENT_COMPLETION требует проверки владельцем файлов текущего завершения.
func ResultDeclarationPrompt(requirements []entity.WorkflowResultRequirement) (string, error) {
	if len(requirements) < 1 || len(requirements) > 4 {
		return "", ErrResultInvalid
	}
	declaration := entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1"}
	seen := make(map[string]bool, len(requirements))
	for _, requirement := range requirements {
		if !resultKey.MatchString(requirement.Key) || seen[requirement.Key] {
			return "", ErrResultInvalid
		}
		seen[requirement.Key] = true
		result := entity.WorkflowDeclaredResult{Key: requirement.Key}
		switch requirement.Kind {
		case "ARTIFACT":
			result.Source = &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}
		case "PULL_REQUEST", "INTEGRATION_RECEIPT", "DEPLOYMENT":
			result.ReceiptRef = "<receiptRef из результата разрешённой интеграции>"
		case "ARTIFACT_REVIEW":
			result.Source = &entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: "<artifactRef принятого subject/result>", Revision: 1, Digest: "<точный digest принятой ревизии>"}
			result.Verdict = "<PASS или CHANGES_REQUIRED по фактической проверке>"
		case "REVIEW", "QA":
			result.HeadSHA = "<точный headSha принятого результата предшественника>"
			result.Verdict = "<PASS или CHANGES_REQUIRED по фактической проверке>"
		default:
			return "", ErrResultInvalid
		}
		declaration.Results = append(declaration.Results, result)
	}
	example, err := json.Marshal(declaration)
	if err != nil {
		return "", ErrResultInvalid
	}
	return "Финальный ответ — только один JSON-объект с полями schema и results, без Markdown и пояснений. " +
		"Каждый обязательный key присутствует ровно один раз. Ниже закрытая форма для назначенного этапа; заменяй обозначения в угловых скобках только фактическими значениями, не копируй их как доказательство.\n" + string(example) + "\n" +
		"У элемента ARTIFACT разрешены только key и source. kind, fileName и mediaType из resultRequirements описывают проверку сервера и не являются полями элемента results. " +
		"В source нового файла текущего завершения допустимо только {\"kind\":\"CURRENT_COMPLETION\"}. " +
		"Для ранее сохранённого файла замени весь source объектом с ровно четырьмя полями: kind со значением CAPTURED_ARTIFACT, artifactRef, revision (целое число) и digest; последние три значения скопируй из свежего неизменяемого каталога RUN_RESULT. " +
		"Не выбирай CURRENT_COMPLETION для файла только из предыдущего технического хода. " +
		"У ARTIFACT_REVIEW разрешены только key, source и verdict; source всегда CAPTURED_ARTIFACT и совпадает с exact subjectStepKey/subjectResultKey из accepted predecessor evidence. Не выбирай файл отчёта рецензента вместо проверенного результата автора. " +
		"У PULL_REQUEST, INTEGRATION_RECEIPT и DEPLOYMENT разрешены только key и receiptRef. У REVIEW и QA — только key, headSha и verdict; verdict равен PASS либо CHANGES_REQUIRED. " +
		"Другие поля, повторные ключи и null запрещены. Ссылки и заявления модели не подтверждают результат: сервер отдельно проверяет capture, доступ, происхождение, сканирование и receipts. " +
		"После принятия обязательных результатов зависимые шаги запускает сервер.", nil
}
