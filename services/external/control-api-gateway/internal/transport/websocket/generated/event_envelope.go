
package generated

type EventEnvelope struct {
  ReservedType string
  RequestRef string
  RunRef string
  Sequence int
  Event *RunEvent
}