package server

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"go.uber.org/zap"

	"mcp-gateway/internal/core"
	genericpb "mcp-gateway/pkg/rpc/proto"
	"mcp-gateway/pkg/rpc/response"
)

// Server implements the GenericServiceServer interface.
type Server struct {
	genericpb.UnimplementedGenericServiceServer
	logger     *zap.Logger
	coreServer *core.Server
}

// Exchange implements the Exchange RPC method for GenericService.
func (s *Server) Exchange(ctx context.Context, in *genericpb.GenericRequest) (*genericpb.GenericResponse, error) {

	fmt.Printf("Received Generic Exchange Request: Code=%s, Server=%s, Module=%s, Method=%s\n",
		in.GetCode(), in.GetServer(), in.GetModule(), in.GetMethod())

	if in.GetCode() == "api_v1_configs_update" {
		if s.coreServer == nil {
			s.logger.Error("core server not initialized in gRPC server")
			return response.NewErrorResponse(in.GetCode(), 500, "Core server not initialized", nil), nil
		}
		err := s.coreServer.UpdateConfigFromRPC(ctx, []byte(in.GetBody()))
		if err != nil {
			s.logger.Error("failed to update configs via RPC", zap.Error(err))
			return response.NewErrorResponse(in.GetCode(), 500, "Failed to update configs via RPC", err), nil
		}
		return response.NewSuccessResponse(in.GetCode(), "Configuration updated successfully via RPC", "Success"), nil
	} else if in.GetCode() == "api_v1_configs_delete" {
		if s.coreServer == nil {
			s.logger.Error("core server not initialized in gRPC server for delete config")
			return response.NewErrorResponse(in.GetCode(), 500, "Core server not initialized", nil), nil
		}
		err := s.coreServer.DeleteConfigFromRPC(ctx, []byte(in.GetBody()))
		if err != nil {
			s.logger.Error("failed to delete configs via RPC", zap.Error(err))
			return response.NewErrorResponse(in.GetCode(), 500, "Failed to delete configs via RPC", err), nil
		}
		return response.NewSuccessResponse(in.GetCode(), "Configuration deleted successfully via RPC", "Success"), nil
	}
	// Example logic: echo back some data or process based on request
	return response.NewSuccessResponse(
		in.GetCode(),
		fmt.Sprintf("Processed request for %s/%s.%s", in.GetServer(), in.GetModule(), in.GetMethod()),
		"Success",
	), nil
}

// Heartbeat implements the Heartbeat RPC method for GenericService.
func (s *Server) Heartbeat(ctx context.Context, in *genericpb.GenericRequest) (*genericpb.GenericResponse, error) {
	fmt.Printf("Received Generic Heartbeat Request: Code=%s, Server=%s\n",
		in.GetCode(), in.GetServer())

	return response.NewSuccessResponse(in.GetCode(), "Heartbeat received", "Success"), nil
}

// Start starts the gRPC server.
func Start(port int, logger *zap.Logger, coreServer *core.Server) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	genericpb.RegisterGenericServiceServer(s, &Server{logger: logger, coreServer: coreServer}) // Register GenericService

	fmt.Printf("gRPC server listening on port %d\n", port)
	if err := s.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %v", err)
	}
	return nil
}
