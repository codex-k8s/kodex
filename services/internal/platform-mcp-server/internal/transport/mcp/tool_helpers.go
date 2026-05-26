package mcptransport

import (
	"context"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func routeOwnerTool[Input any, Request any, Response any, Output any](
	ctx context.Context,
	input Input,
	build func(Input) (Request, error),
	call func(context.Context, Request) (Response, error),
	cast func(Response) Output,
	tool string,
) (*mcpsdk.CallToolResult, Output, error) {
	var empty Output
	request, err := build(input)
	if err != nil {
		return nil, empty, err
	}
	response, err := call(ctx, request)
	if err != nil {
		return nil, empty, ownerToolError(tool, err)
	}
	return nil, cast(response), nil
}

func summarizeItems[Input any, Output any](items []Input, cast func(Input) Output) []Output {
	if len(items) == 0 {
		return nil
	}
	result := make([]Output, 0, len(items))
	for _, item := range items {
		result = append(result, cast(item))
	}
	return result
}

func actorFields(actorType string, actorID string) (string, string, error) {
	trimmedType := strings.TrimSpace(actorType)
	if trimmedType == "" {
		return "", "", invalidInput("actor.type is required")
	}
	trimmedID := strings.TrimSpace(actorID)
	if trimmedID == "" {
		return "", "", invalidInput("actor.id is required")
	}
	return trimmedType, trimmedID, nil
}

func safeRequestSource(source string) (string, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "", invalidInput("request_context.source is required")
	}
	return trimmed, nil
}
