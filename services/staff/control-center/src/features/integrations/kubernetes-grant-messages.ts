export const kubernetesGrantMessages = {
  ru: {
    namespaceLabel: "Область разрешения Kubernetes",
    namespacePlaceholder: "Точный namespace или {cluster}",
    namespaceHelp:
      "Укажите один точный namespace. Для операций над ресурсами кластера — {cluster}. Только отдельные list/watch во всех namespaces принимают {allNamespaces}. Ни одна из областей не выдаёт права на дочерние ресурсы автоматически.",
  },
  en: {
    namespaceLabel: "Kubernetes grant scope",
    namespacePlaceholder: "Exact namespace or {cluster}",
    namespaceHelp:
      "Choose one exact namespace. Use {cluster} for cluster-scoped operations. Only the separate all-namespaces list/watch accept {allNamespaces}. No scope automatically grants access to child resources.",
  },
} as const;
