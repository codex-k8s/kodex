package grpcserver

import (
	"errors"

	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const malformedMarkerField protowire.Number = 536870911

type strictProtoCodec struct{}

func StrictProtoCodec() encoding.Codec {
	return strictProtoCodec{}
}

func (strictProtoCodec) Name() string {
	return "proto"
}

func (strictProtoCodec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, errors.New("gRPC payload is not a protobuf message")
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(message)
}

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

func HasMalformedProto(value any) bool {
	message, ok := value.(proto.Message)
	return !ok || len(message.ProtoReflect().GetUnknown()) != 0
}

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
