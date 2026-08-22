package generated

type SnapshotEnvelope struct {
	ReservedType string
	RequestRef   string
	RunRef       string
	Sequence     int
	Snapshot     *RunGraph
}
