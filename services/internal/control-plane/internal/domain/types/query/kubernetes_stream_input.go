package query

// KubernetesStreamInputList selects a bounded exact run/node surface; actor eligibility is resolved from authenticated owner context for every page.
// KubernetesStreamInputList выбирает bounded exact run/node surface; actor eligibility разрешается из authenticated owner context на каждой странице.
type KubernetesStreamInputList struct {
	RunRef, NodeRef string
	Page            Page
}

// KubernetesStreamInputRead selects one exact stream and optionally one original submission key; neither selector confers input authority or recovers bytes.
// KubernetesStreamInputRead выбирает exact stream и optional original submission key; selectors не выдают input authority и не восстанавливают bytes.
type KubernetesStreamInputRead struct{ InvocationRef, StreamRef, SubmissionIdempotencyKey string }
