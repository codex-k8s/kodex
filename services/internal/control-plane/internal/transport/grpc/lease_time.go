package grpc

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// leaseExpiryOrZero preserves the existing lease-input conversion: absent or invalid protobuf
// timestamps become zero, while valid timestamps retain their exact instant. The owner must
// still validate expiry and the entire lease tuple; this helper does not authenticate a lease.
// leaseExpiryOrZero сохраняет прежнее преобразование lease input: отсутствующий либо неверный
// protobuf timestamp становится нулевым временем, корректный сохраняет точный момент. Владелец
// по-прежнему проверяет expiry и весь lease tuple; helper не удостоверяет lease.
func leaseExpiryOrZero(input *timestamppb.Timestamp) time.Time {
	if input != nil && input.CheckValid() == nil {
		return input.AsTime()
	}
	return time.Time{}
}
