package session

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/pkg/kafka"
	"mcp-gateway/pkg/mcp"
)

// sendSseEventLog sends an SSE event log to Kafka.
func sendSseEventLog(ctx context.Context, meta *Meta, msg *Message, kafkaProducer *kafka.KafkaProducer, logger *zap.Logger, nodeIP string) {
	if kafkaProducer == nil {
		return
	}

	queryJson, _ := json.Marshal(meta.Request.Query)
	logEntry := map[string]any{
		"log_type":           "sse_event",
		"timestamp":          time.Now().Format(time.RFC3339),
		"startTime":          time.Now().Format(time.DateTime),
		"endTime":            time.Now().Format(time.DateTime),
		"session_id":         meta.ID,
		"consumer_token":     meta.ConsumerToken,
		"event_type":         msg.Event,
		"event_data":         string(msg.Data),
		"method":             meta.Request.Headers["Method"],
		"path":               meta.Prefix,
		"query":              string(queryJson),
		"remote_addr":        meta.Request.Headers["X-Forwarded-For"],
		"user_agent":         meta.Request.Headers["User-Agent"],
		"service_identifier": meta.Prefix,
		"node_ip":            nodeIP,
		"client_ip":          meta.Request.ClientIP,
	}

	var jsonRPCResponse mcp.JSONRPCResponse
	if len(msg.Data) > 0 && json.Unmarshal(msg.Data, &jsonRPCResponse) == nil && jsonRPCResponse.ID != nil {
		logEntry["jsonrpc_id"] = jsonRPCResponse.ID
	}

	go func() {
		logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := kafkaProducer.Produce(logCtx, cnst.KafkaTopicSseEvent, meta.Prefix, logEntry)
		if err != nil {
			logger.Error("failed to send SSE event log to Kafka", zap.Error(err), zap.String("session_id", meta.ID))
		}
	}()
}
