export const kubernetesChildGateMessages = {
  ru: {
    title: "Дочернее действие Kubernetes",
    submittedChanges: "Отправленные изменения",
    submittedOnly:
      "Показаны известные значения отправленного запроса. Предыдущее состояние Kubernetes не загружено; это не полный diff.",
    hidden: "Значение скрыто или его семантика не подтверждена.",
    protectedMaterial: "Ссылка на защищённый материал Secret:",
    limitations: {
      PORT_FORWARD_LOGICAL_STREAM_16_ACTIVE:
        "Допуск относится к одному TCP-туннелю с указанными портами. Повторные и последовательные соединения разрешены: одновременно не более 16, в пределах времени и общего объёма потока. Каждое соединение проходит свежую проверку допуска; закрытие владельцем не доказывает завершение удалённой стороны.",
      NOT_AUTHORIZED:
        "В этом представлении значения скрыты. Откройте запрос с правом решения.",
      SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN:
        "Часть полей скрыта: содержимое чувствительное или его семантика не подтверждена.",
      PROTECTED_MATERIAL_HIDDEN:
        "Значения Secret не раскрываются. Решение относится к указанной ссылке на защищённый материал.",
      BUDGET_EXCEEDED:
        "Часть представления не помещается в ответ. Откройте запрос для подробностей; скрытое содержимое не считается проверенным.",
    },
    question: "Разрешить одно действие для указанного ресурса исходного плана?",
    invalid: "Сведения о действии недоступны. Обновите решение.",
    singleAction:
      "Одобрение относится к одному действию исходного плана. Перед исполнением сервер повторно проверит исходный допуск.",
    operation: "Операция",
    target: "Цель",
    revision: "ревизия {value}",
    resource: "Ресурс API",
    scope: "Область",
    cluster: "Область кластера",
    name: "Имя объекта",
    intendedName: "Имя создаваемого объекта",
    generatedPrefix: "Префикс генерируемого имени",
    notCreated: "Окончательное имя назначит Kubernetes при создании объекта.",
    plan: "Исходный план",
    child: "Действие плана",
    planDigest: "SHA256 плана",
    inputDigest: "SHA256 входа действия",
  },
  en: {
    title: "Kubernetes child action",
    submittedChanges: "Submitted changes",
    submittedOnly:
      "Known submitted values are shown. The previous Kubernetes state was not loaded; this is not a complete diff.",
    hidden: "Value hidden or its semantics are unverified.",
    protectedMaterial: "Protected Secret material reference:",
    limitations: {
      PORT_FORWARD_LOGICAL_STREAM_16_ACTIVE:
        "Approval covers one TCP tunnel with the specified ports. Repeated and sequential connections are allowed: at most 16 active, within the stream time and total-byte limits. Each connection needs fresh authorization; owner closure does not prove remote termination.",
      NOT_AUTHORIZED:
        "This view hides values. Open the request with permission to resolve it.",
      SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN:
        "Some fields are hidden because their content is sensitive or their semantics are unverified.",
      PROTECTED_MATERIAL_HIDDEN:
        "Secret values remain hidden. This decision binds the protected material reference shown here.",
      BUDGET_EXCEEDED:
        "Some details exceed the response budget. Open the request for details; hidden content is not considered reviewed.",
    },
    question: "Allow one action on this resource from the original plan?",
    invalid: "Action details are unavailable. Refresh the decision.",
    singleAction:
      "Approval covers one action in the original plan. The server rechecks the original authorization before execution.",
    operation: "Operation",
    target: "Target",
    revision: "revision {value}",
    resource: "API resource",
    scope: "Scope",
    cluster: "Cluster scope",
    name: "Object name",
    intendedName: "Proposed object name",
    generatedPrefix: "Generated-name prefix",
    notCreated: "Kubernetes assigns the final name when creating the object.",
    plan: "Original plan",
    child: "Plan action",
    planDigest: "Plan SHA256",
    inputDigest: "Action input SHA256",
  },
};
