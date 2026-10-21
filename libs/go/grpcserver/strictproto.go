package grpcserver

import (
	"errors"

	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// malformedMarkerField is a reserved local unknown-field tag that marks a
// rejected wire payload for the following server interceptor.
// RU: malformedMarkerField — локальная метка неизвестного поля, которой
// помечается отвергнутый wire payload для следующего interceptor.
const malformedMarkerField protowire.Number = 536870911

// strictProtoCodec implements the gRPC proto codec with a pre-decode wire
// scan that rejects duplicate singular fields and unknown fields.
// RU: strictProtoCodec реализует codec gRPC proto с предварительной проверкой
// wire-данных на повторные одиночные и неизвестные поля.
type strictProtoCodec struct{}

// StrictProtoCodec returns a codec that validates incoming protobuf wire
// shape before an application interceptor accepts the message.
// RU: StrictProtoCodec возвращает codec для проверки входного формата
// protobuf до принятия сообщения прикладным interceptor.
func StrictProtoCodec() encoding.Codec {
	return strictProtoCodec{}
}

// Name registers this strict implementation under the standard proto codec name.
// RU: Name регистрирует строгую реализацию под стандартным именем codec proto.
func (strictProtoCodec) Name() string {
	return "proto"
}

// Marshal accepts only protobuf messages and emits deterministic outbound
// bytes without weakening inbound validation.
// RU: Marshal принимает только protobuf-сообщения и выдаёт детерминированные
// исходящие байты, не ослабляя проверку входящих данных.
func (strictProtoCodec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, errors.New("gRPC payload is not a protobuf message")
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(message)
}

// Unmarshal performs both wire-shape and protobuf decoding checks; on any
// failure it clears partial state and marks the message for interceptor rejection.
// RU: Unmarshal проверяет форму wire и декодирование protobuf; при ошибке
// очищает частичное состояние и помечает сообщение для отказа interceptor.
func (strictProtoCodec) Unmarshal(raw []byte, value any) error {
	message, ok := value.(proto.Message)
	if !ok {
		return errors.New("gRPC payload is not a protobuf message")
	}
	wireErr := validateWire(message.ProtoReflect().Descriptor(), raw)
	decodeErr := proto.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(raw, message)
	if wireErr == nil && decodeErr == nil {
		return nil
	}
	proto.Reset(message)
	marker := protowire.AppendTag(nil, malformedMarkerField, protowire.VarintType)
	marker = protowire.AppendVarint(marker, 1)
	message.ProtoReflect().SetUnknown(marker)
	return nil
}

// HasMalformedProto rejects non-protobuf values and any message carrying
// unknown fields, including the strict codec's malformed marker.
// RU: HasMalformedProto отклоняет не-protobuf значения и сообщения с
// неизвестными полями, включая метку ошибки строгого codec.
func HasMalformedProto(value any) bool {
	message, ok := value.(proto.Message)
	return !ok || len(message.ProtoReflect().GetUnknown()) != 0
}

// validateWire recursively checks known field numbers, unique singular and
// oneof members, valid field values and nested message wire types.
// RU: validateWire рекурсивно проверяет известные номера полей, уникальность
// одиночных и oneof-полей, значения и тип wire вложенных сообщений.
func validateWire(descriptor protoreflect.MessageDescriptor, raw []byte) error {
	seenFields := make(map[protowire.Number]struct{})
	seenOneofs := make(map[protoreflect.FullName]struct{})
	for len(raw) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(raw)
		if tagLength < 0 || number <= 0 {
			return errors.New("protobuf tag is malformed")
		}
		raw = raw[tagLength:]
		field := descriptor.Fields().ByNumber(protoreflect.FieldNumber(number))
		if field == nil {
			return errors.New("protobuf field is unknown")
		}
		if field.Cardinality() != protoreflect.Repeated {
			if _, duplicate := seenFields[number]; duplicate {
				return errors.New("protobuf singular field is duplicated")
			}
			seenFields[number] = struct{}{}
		}
		if oneof := field.ContainingOneof(); oneof != nil {
			if _, duplicate := seenOneofs[oneof.FullName()]; duplicate {
				return errors.New("protobuf oneof is duplicated")
			}
			seenOneofs[oneof.FullName()] = struct{}{}
		}
		valueLength := protowire.ConsumeFieldValue(number, wireType, raw)
		if valueLength < 0 {
			return errors.New("protobuf field value is malformed")
		}
		if field.Kind() == protoreflect.MessageKind {
			if wireType != protowire.BytesType {
				return errors.New("protobuf message field has invalid wire type")
			}
			nested, nestedLength := protowire.ConsumeBytes(raw)
			if nestedLength < 0 || nestedLength != valueLength {
				return errors.New("protobuf message field is malformed")
			}
			if err := validateWire(field.Message(), nested); err != nil {
				return err
			}
		}
		raw = raw[valueLength:]
	}
	return nil
}
