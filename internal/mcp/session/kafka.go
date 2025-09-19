package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/internal/common/config"
	"mcp-gateway/pkg/kafka"
)

// SessionEvent represents a session lifecycle event for Kafka
type SessionEvent struct {
	Action  string   `json:"action"` // "create", "delete"
	Meta    *Meta    `json:"meta"`
	Message *Message `json:"message,omitempty"`
}

// KafkaConnection represents a session connection managed by Kafka.
type KafkaConnection struct {
	store       *KafkaStore
	meta        *Meta
	eventQueue  chan *Message
	producer    *kafka.KafkaProducer // This producer is for outbound messages to specific session topics
	logger      *zap.Logger
	topicPrefix string
	nodeIP      string
}

// NewKafkaConnection creates a new KafkaConnection.
func NewKafkaConnection(store *KafkaStore, logger *zap.Logger, producer *kafka.KafkaProducer, meta *Meta, topicPrefix string, nodeIP string) *KafkaConnection {
	return &KafkaConnection{
		store:       store,
		meta:        meta,
		eventQueue:  make(chan *Message, 100), // Buffered channel for outbound messages
		producer:    producer,
		logger:      logger.With(zap.String("session_id", meta.ID)),
		topicPrefix: topicPrefix,
		nodeIP:      nodeIP,
	}
}

// EventQueue returns a read-only channel where outbound messages are published.
func (c *KafkaConnection) EventQueue() <-chan *Message {
	return c.eventQueue
}

// Send pushes a message to the session.
func (c *KafkaConnection) Send(ctx context.Context, msg *Message) error {
	// Prepare log entry for Kafka
	if c.producer != nil { // Assuming this producer is for general logs, not session-specific messages
		queryJson, _ := json.Marshal(c.meta.Request.Query)
		logEntry := map[string]any{
			"log_type":           "sse_event",
			"timestamp":          time.Now().Format(time.RFC3339),
			"startTime":          time.Now().Format(time.DateTime),
			"endTime":            time.Now().Format(time.DateTime),
			"session_id":         c.meta.ID,
			"consumer_token":     c.meta.ConsumerToken,
			"event_type":         msg.Event,
			"event_data":         string(msg.Data),
			"method":             c.meta.Request.Headers["Method"],
			"path":               c.meta.Prefix,
			"query":              string(queryJson),
			"remote_addr":        c.meta.Request.Headers["X-Forwarded-For"],
			"user_agent":         c.meta.Request.Headers["User-Agent"],
			"service_identifier": c.meta.Prefix,
			"node_ip":            c.nodeIP,
		}

		go func() {
			logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Use the log producer for logging
			err := c.producer.Produce(logCtx, cnst.KafkaTopicSseEvent, c.meta.Prefix, logEntry)
			if err != nil {
				c.logger.Error("failed to send SSE event log to Kafka", zap.Error(err), zap.String("session_id", c.meta.ID))
			}
		}()
	}

	if err := c.store.publishSessionEvent(ctx, "event", c.meta, msg); err != nil {
		c.logger.Error("Failed to publish session create event", zap.Error(err))
		return fmt.Errorf("failed to publish session create event: %w", err)
	}

	return nil
}

// Close gracefully terminates the session connection.
func (c *KafkaConnection) Close(ctx context.Context) error {
	c.logger.Info("Closing Kafka session connection")
	close(c.eventQueue)
	// In a real-world scenario, you might send a "session_closed" event to Kafka
	// or perform other cleanup. For now, just close the channel.
	return nil
}

// Meta returns metadata associated with the session.
func (c *KafkaConnection) Meta() *Meta {
	return c.meta
}

// KafkaStore implements the Store interface for Kafka-based session management.
type KafkaStore struct {
	logger      *zap.Logger
	logProducer *kafka.KafkaProducer // Producer for general logs (e.g., SSE events)
	msgProducer *kafka.KafkaProducer // Producer for session lifecycle events and outbound messages
	consumer    *kafka.KafkaConsumer // Consumer for session lifecycle events
	connections map[string]*KafkaConnection
	mu          sync.RWMutex // Mutex to protect the connections map
	nodeIP      string
	topicPrefix string
	cfg         *config.SessionKafkaConfig // Store config for consumer initialization
	cancelCtx   context.CancelFunc         // To cancel consumer goroutine
}

// NewKafkaStore creates a new KafkaStore.
func NewKafkaStore(logger *zap.Logger, logProducer *kafka.KafkaProducer, cfg *config.SessionKafkaConfig, nodeIP string) (*KafkaStore, error) {
	if logProducer == nil {
		return nil, fmt.Errorf("kafka log producer is nil")
	}
	if cfg == nil || len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka session configuration not found or incomplete")
	}

	msgProducer := kafka.NewKafkaProducer(
		&kafka.ProducerConfig{
			Brokers: cfg.Brokers,
		},
		logger,
	)
	logger.Info("Kafka msg producer initialized", zap.Strings("brokers", cfg.Brokers))

	consumer, err := kafka.NewKafkaConsumer(
		&kafka.ConsumerConfig{
			Brokers: cfg.Brokers,
			Topic:   cnst.KafkaTopicSessionEvent,
			GroupID: fmt.Sprintf("%s-%s", cnst.AppName, nodeIP), // Unique consumer group per instance
		},
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer for session events: %w", err)
	}
	logger.Info("Kafka session event consumer initialized", zap.Strings("brokers", cfg.Brokers), zap.String("topic", cnst.KafkaTopicSessionEvent))

	store := &KafkaStore{
		logger:      logger.With(zap.String("store_type", "kafka")),
		logProducer: logProducer,
		msgProducer: msgProducer,
		consumer:    consumer,
		connections: make(map[string]*KafkaConnection),
		nodeIP:      nodeIP,
		topicPrefix: cfg.TopicPrefix,
		cfg:         cfg,
	}

	// Start goroutine to handle session events
	ctx, cancel := context.WithCancel(context.Background())
	store.cancelCtx = cancel
	go store.handleSessionEvents(ctx)

	return store, nil
}

// handleSessionEvents consumes session lifecycle events from Kafka
func (s *KafkaStore) handleSessionEvents(ctx context.Context) {
	s.logger.Info("Starting Kafka session event consumer")
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Kafka session event consumer stopped")
			return
		default:
			msg, err := s.consumer.ReadMessage(ctx)
			if err != nil {
				if err.Error() == "context canceled" { // Check for context cancellation specifically
					s.logger.Info("Kafka consumer context canceled, stopping handleSessionEvents")
					return
				}
				s.logger.Error("Failed to read Kafka session event message", zap.Error(err))
				time.Sleep(time.Second) // Wait before retrying
				continue
			}

			var event SessionEvent
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				s.logger.Error("Failed to unmarshal session event", zap.Error(err), zap.ByteString("value", msg.Value))
				continue
			}

			s.mu.Lock()
			switch event.Action {
			case "create":
				s.logger.Debug("Received session create event", zap.String("session_id", event.Meta.ID))
				// Create a dummy connection for local tracking, actual connection will be established on Get
				conn := NewKafkaConnection(s, s.logger, s.msgProducer, event.Meta, s.topicPrefix, s.nodeIP)
				s.connections[event.Meta.ID] = conn
			case "delete":
				s.logger.Debug("Received session delete event", zap.String("session_id", event.Meta.ID))
				if conn, ok := s.connections[event.Meta.ID]; ok {
					_ = conn.Close(ctx) // Close the local connection gracefully
					delete(s.connections, event.Meta.ID)
				}
			case "event":
				s.logger.Debug("Received session outbound message event", zap.String("session_id", event.Meta.ID))
				if conn, ok := s.connections[event.Meta.ID]; ok {
					conn.eventQueue <- event.Message
				}
			default:
				s.logger.Warn("Received unknown session event action", zap.String("action", event.Action), zap.String("session_id", event.Meta.ID))
			}
			s.mu.Unlock()
		}
	}
}

// publishSessionEvent publishes a session lifecycle event to Kafka
func (s *KafkaStore) publishSessionEvent(ctx context.Context, action string, meta *Meta, msg *Message) error {
	event := SessionEvent{
		Action:  action,
		Meta:    meta,
		Message: msg,
	}
	eventBytes, err := json.Marshal(event)
	if err != nil {
		s.logger.Error("Failed to marshal session event", zap.Error(err))
		return fmt.Errorf("failed to marshal session event: %w", err)
	}

	err = s.msgProducer.Produce(ctx, cnst.KafkaTopicSessionEvent, meta.ID, eventBytes)
	if err != nil {
		s.logger.Error("Failed to publish session event to Kafka", zap.Error(err), zap.String("action", action), zap.String("session_id", meta.ID))
		return fmt.Errorf("failed to publish session event: %w", err)
	}
	return nil
}

// Register creates and registers a new session connection.
func (s *KafkaStore) Register(ctx context.Context, meta *Meta) (Connection, error) {
	s.logger.Info("Registering new Kafka session", zap.String("session_id", meta.ID))

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if connection already exists locally
	if _, exists := s.connections[meta.ID]; exists {
		return nil, fmt.Errorf("connection already exists: %s", meta.ID)
	}

	conn := NewKafkaConnection(s, s.logger, s.msgProducer, meta, s.topicPrefix, s.nodeIP)
	s.connections[meta.ID] = conn

	// Publish session creation event to Kafka
	if err := s.publishSessionEvent(ctx, "create", meta, nil); err != nil {
		s.logger.Error("Failed to publish session create event", zap.Error(err))
		return nil, fmt.Errorf("failed to publish session create event: %w", err)
	}

	return conn, nil
}

// Get retrieves an active session connection by ID.
func (s *KafkaStore) Get(ctx context.Context, id string) (Connection, error) {
	s.mu.RLock()
	conn, ok := s.connections[id]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return conn, nil
}

// Unregister removes a session connection by ID.
func (s *KafkaStore) Unregister(ctx context.Context, id string) error {
	s.logger.Info("Unregistering Kafka session", zap.String("session_id", id))

	s.mu.Lock()
	conn, ok := s.connections[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("session not found: %s", id)
	}
	delete(s.connections, id)
	s.mu.Unlock()

	_ = conn.Close(ctx) // Close the connection gracefully

	// Publish session deletion event to Kafka
	meta := &Meta{ID: id} // Only ID is needed for deletion event
	if err := s.publishSessionEvent(ctx, "delete", meta, nil); err != nil {
		s.logger.Error("Failed to publish session delete event", zap.Error(err))
		return fmt.Errorf("failed to publish session delete event: %w", err)
	}

	return nil
}

// List returns all currently active session connections.
func (s *KafkaStore) List(ctx context.Context) ([]Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	connections := make([]Connection, 0, len(s.connections))
	for _, conn := range s.connections {
		connections = append(connections, conn)
	}
	return connections, nil
}

// Close closes the Kafka store
func (s *KafkaStore) Close() error {
	s.logger.Info("Closing KafkaStore")
	if s.cancelCtx != nil {
		s.cancelCtx() // Stop the consumer goroutine
	}
	if s.consumer != nil {
		if err := s.consumer.Close(); err != nil {
			s.logger.Error("Failed to close Kafka consumer", zap.Error(err))
			return fmt.Errorf("failed to close Kafka consumer: %w", err)
		}
	}
	if s.msgProducer != nil {
		if err := s.msgProducer.Close(); err != nil {
			s.logger.Error("Failed to close Kafka message producer", zap.Error(err))
			return fmt.Errorf("failed to close Kafka message producer: %w", err)
		}
	}
	// logProducer is managed externally, so we don't close it here.
	return nil
}
