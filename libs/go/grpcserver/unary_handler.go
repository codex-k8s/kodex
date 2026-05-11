package grpcserver

import "context"

// UnaryCaster maps one transport request to one domain input.
type UnaryCaster[Request any, Input any] func(*Request) (Input, error)

// UnaryCaller executes one domain use case.
type UnaryCaller[Input any, Output any] func(context.Context, Input) (Output, error)

// UnaryResponder maps one domain output to one transport response.
type UnaryResponder[Output any, Response any] func(Output) *Response

// HandleUnary keeps gRPC handlers free from repeated caster/call/response plumbing.
func HandleUnary[Request any, Input any, Output any, Response any](
	ctx context.Context,
	request *Request,
	cast UnaryCaster[Request, Input],
	call UnaryCaller[Input, Output],
	respond UnaryResponder[Output, Response],
) (*Response, error) {
	domainInput, err := cast(request)
	if err != nil {
		return nil, err
	}
	domainOutput, err := call(ctx, domainInput)
	if err != nil {
		return nil, err
	}
	return respond(domainOutput), nil
}

// PairCaster maps one transport request to a pair of domain arguments.
type PairCaster[Request any, First any, Second any] func(*Request) (First, Second, error)

// PairCaller executes a domain use case with two arguments.
type PairCaller[First any, Second any, Output any] func(context.Context, First, Second) (Output, error)

// HandleUnaryPair keeps gRPC handlers free from repeated two-argument plumbing.
func HandleUnaryPair[Request any, First any, Second any, Output any, Response any](
	ctx context.Context,
	request *Request,
	cast PairCaster[Request, First, Second],
	call PairCaller[First, Second, Output],
	respond UnaryResponder[Output, Response],
) (*Response, error) {
	first, second, err := cast(request)
	if err != nil {
		return nil, err
	}
	domainOutput, err := call(ctx, first, second)
	if err != nil {
		return nil, err
	}
	return respond(domainOutput), nil
}
