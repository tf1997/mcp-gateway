package cnst

const (
	AppName     = "mcp-gateway"
	CommandName = "mcp-gateway"
)

// Kafka topics
const (
	KafkaTopicHttpRequest  = "mcp-gateway-http-logs"
	KafkaTopicSseEvent     = "mcp-gateway-sse-events-log"
	KafkaTopicSessionEvent = "mcp-gateway-session-events" // New Kafka topic for session events
)
