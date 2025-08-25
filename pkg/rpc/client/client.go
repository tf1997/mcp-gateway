package client

import (
	"context"
	"fmt"
	"mcp-gateway/internal/common/config"
	pb "mcp-gateway/pkg/rpc/proto"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"go.uber.org/zap"

)

func Heartbeat(logger *zap.Logger, cfg *config.MCPGatewayConfig, nodeIp string) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := heartbeat(cfg, nodeIp); err != nil {
			logger.Error("Error in Heartbeat: %v", zap.Any("err", err))
		}
	}
}

func heartbeat(cfg *config.MCPGatewayConfig, nodeIp string) error {
	conn, err := grpc.NewClient(cfg.ClusterManager, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewGenericServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.Heartbeat(ctx, &pb.GenericRequest{Code: "mcp_gateway", ClientIp: nodeIp, Body: fmt.Sprintf("%d#%d#%s", cfg.Port, cfg.RPCPort, cfg.Env)})
	if err != nil {
		return fmt.Errorf("could not link to grpc server: %v", err)
	}
	if r.GetStatus() != 0 {
		return fmt.Errorf("heartbeat failed: %s", r.GetMessage())
		
	}
	return nil
}