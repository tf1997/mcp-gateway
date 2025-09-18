package cnst

const (
	AppName     = "mcp-gateway"
	CommandName = "mcp-gateway"
)

// Kafka topics
const (
	KafkaTopicHttpRequest  = "mcp_gateway_http_logs"
	KafkaTopicSseEvent     = "mcp_gateway_sse_events_log"
	KafkaTopicSessionEvent = "mcp_gateway_session_events" // New Kafka topic for session events
)
