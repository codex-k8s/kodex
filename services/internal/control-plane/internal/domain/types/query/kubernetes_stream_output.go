package query

// KubernetesStreamOutputRead selects the next bounded persisted frame without accepting actor or storage authority.
// KubernetesStreamOutputRead выбирает следующий сохранённый кадр без actor либо storage authority из запроса.
type KubernetesStreamOutputRead struct {
	InvocationRef, StreamRef string
	Generation               int64
	AfterSequence            uint64
}
