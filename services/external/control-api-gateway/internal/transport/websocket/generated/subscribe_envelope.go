
package generated

type SubscribeEnvelope struct {
  ReservedType *SubscribeMessageType
  RequestId string
  Channels []ProjectionChannel
  ResourceKinds []ResourceKind
}