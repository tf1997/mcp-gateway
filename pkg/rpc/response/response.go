package response

import (
	"fmt"
	"time"

	genericpb "mcp-gateway/pkg/rpc/proto"
)

// NewSuccessResponse creates a new GenericResponse for a successful operation.
func NewSuccessResponse(code, data, message string) *genericpb.GenericResponse {
	return &genericpb.GenericResponse{
		Code:    code,
		NodeIp:  "127.0.0.1", // TODO: Make this configurable or derive from context
		Ts:      time.Now().Unix(),
		Status:  200,
		Data:    data,
		Message: message,
	}
}

// NewErrorResponse creates a new GenericResponse for a failed operation.
func NewErrorResponse(code string, status int32, message string, err error) *genericpb.GenericResponse {
	errMsg := message
	if err != nil {
		errMsg = fmt.Sprintf("%s: %v", message, err)
	}
	return &genericpb.GenericResponse{
		Code:    code,
		NodeIp:  "127.0.0.1", // TODO: Make this configurable or derive from context
		Ts:      time.Now().Unix(),
		Status:  status,
		Data:    "", // No data on error
		Message: errMsg,
	}
}
