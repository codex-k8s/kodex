package query

// KubernetesToolList selects an exact run/node; each returned invocation requires current original-root visibility.
// KubernetesToolList выбирает точные run/node; каждый вызов проверяется по текущим правам исходного инициатора.
type KubernetesToolList struct {
	RunRef, NodeRef string
	Page            Page
}
