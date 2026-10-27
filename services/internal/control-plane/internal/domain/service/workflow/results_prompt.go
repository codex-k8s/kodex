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
	return ResultDeclarationPromptForProfile(requirements, "")
}

// ResultDeclarationPromptForProfile discloses the exact opted-in receipt/report contract without
// creating evidence or authorizing GitHub approval; old profile text remains byte-identical.
// ResultDeclarationPromptForProfile показывает точный выбранный receipt/report контракт без
// создания evidence либо GitHub approval; текст прежнего профиля остаётся byte-identical.
func ResultDeclarationPromptForProfile(requirements []entity.WorkflowResultRequirement, profile string) (string, error) {
	if profile != "" && profile != GitHubReviewQAV1 {
		return "", ErrResultInvalid
	}
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
		case "REVIEW", "QA":
			result.HeadSHA = "<точный headSha принятого результата предшественника>"
			result.Verdict = "<PASS или CHANGES_REQUIRED по фактической проверке>"
			if profile == GitHubReviewQAV1 && requirement.Kind == "REVIEW" {
				result.ReceiptRef = "<receiptRef native COMMENT на exact accepted PR/head>"
			}
		default:
			return "", ErrResultInvalid
		}
		declaration.Results = append(declaration.Results, result)
	}
	example, err := json.Marshal(declaration)
	if err != nil {
		return "", ErrResultInvalid
	}
	prefix := ""
	reviewShape := "У REVIEW и QA — только key, headSha и verdict; verdict равен PASS либо CHANGES_REQUIRED. "
	if profile == GitHubReviewQAV1 {
		reviewShape = "У REVIEW — только key, headSha, verdict и receiptRef; у QA — key, headSha, verdict и optional receiptRef только для созданного Issue при CHANGES_REQUIRED. Verdict равен PASS либо CHANGES_REQUIRED. "
		prefix = "Явный профиль GITHUB_REVIEW_QA_V1. REVIEW требует receiptRef успешного native GitHub COMMENT на точный PR/head принятого subject. COMMENT не является GitHub APPROVE; event APPROVE и REQUEST_CHANGES здесь запрещены. QA всегда связан через evidenceKey с опубликованным ARTIFACT application/json: closed schema kodex.workflow-qa-report.v1 с exact repository/pullRequestNumber/headSha/verdict, tests (name,command,exitCode,log CAPTURED_ARTIFACT pins), findings(key,summary) и optional issue(title,body). Report/logs подтверждают bytes и producing execution, не автоматически факт выполнения тестов. Выполни реальные команды и сохрани полный вывод; если выполнить нельзя, честно сообщи failure, не выдумывай PASS/дефект. Для Issue сначала сохрани report/logs через CAPTURE_PROGRESS и получи fresh RR. Issue разрешён только для CHANGES_REQUIRED с findings и ровно теми title/body, которые сохранены в report; один Issue на business attempt, неизвестный исход только читать, не повторять. У QA receiptRef допустим только при реально созданном Issue и CHANGES_REQUIRED. После UNKNOWN либо pending не публикуй новый effect. У REVIEW поля key/headSha/verdict/receiptRef; у QA key/headSha/verdict и optional receiptRef. \n"
	}
	if profile == GitHubReviewQAV1 {
		for _, requirement := range requirements {
			if requirement.Kind == "QA" {
				prefix += qaReportGuidance
				break
			}
		}
	}
	return prefix + "Финальный ответ — только один JSON-объект с полями schema и results, без Markdown и пояснений. " +
		"Каждый обязательный key присутствует ровно один раз. Ниже закрытая форма для назначенного этапа; заменяй обозначения в угловых скобках только фактическими значениями, не копируй их как доказательство.\n" + string(example) + "\n" +
		"У элемента ARTIFACT разрешены только key и source. kind, fileName и mediaType из resultRequirements описывают проверку сервера и не являются полями элемента results. " +
		"В source нового файла текущего завершения допустимо только {\"kind\":\"CURRENT_COMPLETION\"}. " +
		"Для ранее сохранённого файла замени весь source объектом с ровно четырьмя полями: kind со значением CAPTURED_ARTIFACT, artifactRef, revision (целое число) и digest; последние три значения скопируй из свежего неизменяемого каталога RUN_RESULT. " +
		"Не выбирай CURRENT_COMPLETION для файла только из предыдущего технического хода. " +
		"У PULL_REQUEST, INTEGRATION_RECEIPT и DEPLOYMENT разрешены только key и receiptRef. " + reviewShape +
		"Другие поля, повторные ключи и null запрещены. Ссылки и заявления модели не подтверждают результат: сервер отдельно проверяет capture, доступ, происхождение, сканирование и receipts. " +
		"После принятия обязательных результатов зависимые шаги запускает сервер.", nil
}

// qaReportGuidance makes the closed report and its reachable capture sequence explicit; all
// external identities remain invalid placeholders and cannot serve as evidence without owner proof.
// qaReportGuidance явно показывает closed report и достижимую последовательность capture;
// external identities остаются невалидными placeholders без owner proof.
const qaReportGuidance = `QA report — только один JSON object, максимум32768 UTF-8 bytes, без Markdown/дополнительных полей. Tests от1 до8, findings от0 до8 с уникальным key. Name до160 символов, command до2048, exitCode целое0..255, finding summary до2000, Issue title до256/body до8192. При PASS каждый exitCode=0, findings=[], issue отсутствует. Сначала создай полный вывод реальных команд и CAPTURE_PROGRESS с опубликованными ARTIFACT keys; только после fresh RR скопируй exact log refs/revision/digest из RUN_RESULT. Затем создай report. При PASS report может быть CURRENT_COMPLETION в final declaration. Для Issue report сначала отдельно CAPTURE_PROGRESS, затем ещё fresh RR и разрешённый invoke; в final report выбирается CAPTURED_ARTIFACT. Не придумывай refs, числа, digest или факт запуска. Если действие недоступно — честное отклонение без фиктивного Issue. Закрытый пример report ниже содержит намеренно невалидные placeholders: pullRequestNumber/revision замени целыми JSON numbers из фактических pins, не строками.
{"schema":"kodex.workflow-qa-report.v1","repository":"<repository принятого PR>","pullRequestNumber":"<целое число принятого PR>","headSha":"<точный headSha>","verdict":"PASS","tests":[{"name":"<название реальной проверки>","command":"<фактически выполненная команда>","exitCode":0,"log":{"kind":"CAPTURED_ARTIFACT","artifactRef":"<artifactRef из свежего RUN_RESULT>","revision":"<целая revision из каталога>","digest":"<sha256:... из каталога>"}}],"findings":[]}
При реальном CHANGES_REQUIRED замени verdict, заполни findings только объектами {"key":"<уникальный ключ>","summary":"<точное наблюдение>"}; optional issue — только {"title":"<заголовок совокупного дефекта>","body":"<фактические шаги и evidence>"}. Report/logs являются данными сотрудника, а не аттестацией сервера о выполнении тестов.
`
