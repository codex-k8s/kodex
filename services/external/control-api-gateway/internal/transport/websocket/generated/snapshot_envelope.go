
package generated

type SnapshotEnvelope struct {
  ReservedType *SnapshotMessageType
  RequestId string
  Channel *ProjectionChannel
  Sequence int
  SnapshotId string
  Complete bool
  ServerTime string
  Items *SnapshotItems
}