package integration

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RepositoryBundleWireKnown rejects unknown protobuf fields recursively at this specialized owner/consumer boundary, including nested pins, timestamps and Struct values; it does not validate authority or semantic state.
// RepositoryBundleWireKnown рекурсивно отклоняет unknown protobuf fields на specialized owner/consumer boundary, включая nested pins/timestamps/Struct values; authority и semantic state отдельно проверяются caller.
func RepositoryBundleWireKnown(message proto.Message) bool {
	if message == nil || !message.ProtoReflect().IsValid() {
		return false
	}
	return repositoryBundleMessageKnown(message.ProtoReflect())
}

// repositoryBundleMessageKnown walks only populated message fields; bounded owner transport sizes constrain the input and no application fields are silently normalized.
// repositoryBundleMessageKnown обходит лишь populated message fields; размер ограничен owner transport, application fields не нормализуются молча.
func repositoryBundleMessageKnown(message protoreflect.Message) bool {
	if len(message.GetUnknown()) != 0 {
		return false
	}
	valid := true
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsMap():
			if field.MapValue().Kind() == protoreflect.MessageKind {
				value.Map().Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool {
					valid = repositoryBundleMessageKnown(v.Message())
					return valid
				})
			}
		case field.IsList():
			if field.Kind() == protoreflect.MessageKind {
				for i := 0; i < value.List().Len(); i++ {
					if !repositoryBundleMessageKnown(value.List().Get(i).Message()) {
						valid = false
						break
					}
				}
			}
		case field.Kind() == protoreflect.MessageKind:
			valid = repositoryBundleMessageKnown(value.Message())
		}
		return valid
	})
	return valid
}
