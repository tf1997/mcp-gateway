package session

import (
	"context"
	"fmt"
	"sync"
	"time" // Add time import

	"go.uber.org/zap"

	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/pkg/kafka" // Add kafka import
)

// MemoryStore implements Store using in-memory storage
type MemoryStore struct {
	logger        *zap.Logger
	mu            sync.RWMutex
	conns         map[string]Connection
	kafkaProducer *kafka.KafkaProducer // Add Kafka producer
	nodeIP        string               // Add node IP for logging
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore creates a new in-memory session store
func NewMemoryStore(logger *zap.Logger, kafkaProducer *kafka.KafkaProducer, nodeIP string) *MemoryStore {
	return &MemoryStore{
		logger:        logger.Named("session.store.memory"),
		conns:         make(map[string]Connection),
		kafkaProducer: kafkaProducer,
		nodeIP:        nodeIP,
	}
}

// Register implements Store.Register
func (s *MemoryStore) Register(_ context.Context, meta *Meta) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if connection already exists
	if _, exists := s.conns[meta.ID]; exists {
		return nil, fmt.Errorf("connection already exists: %s", meta.ID)
	}

	// Create new connection
	conn := &MemoryConnection{
		meta:          meta,
		queue:         make(chan *Message, 100),
		kafkaProducer: s.kafkaProducer, // Pass Kafka producer to connection
		nodeIP:        s.nodeIP,        // Pass node IP to connection
		logger:        s.logger,
	}

	// Store connection
	s.conns[meta.ID] = conn

	return conn, nil
}

// Get implements Store.Get
func (s *MemoryStore) Get(_ context.Context, id string) (Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conn, ok := s.conns[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return conn, nil
}

// Unregister implements Store.Unregister
func (s *MemoryStore) Unregister(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, ok := s.conns[id]
	if !ok {
		return ErrSessionNotFound
	}

	// Close connection
	if err := conn.Close(context.Background()); err != nil {
		s.logger.Error("failed to close connection",
			zap.String("id", id),
			zap.Error(err))
	}

	// Remove connection
	delete(s.conns, id)
	return nil
}

// List implements Store.List
func (s *MemoryStore) List(_ context.Context) ([]Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conns := make([]Connection, 0, len(s.conns))
	for _, conn := range s.conns {
		conns = append(conns, conn)
	}
	return conns, nil
}

// MemoryConnection implements Connection using in-memory storage
type MemoryConnection struct {
	meta          *Meta
	queue         chan *Message
	kafkaProducer *kafka.KafkaProducer // Add Kafka producer
	logger        *zap.Logger          // Add logger for logging within connection
	nodeIP        string               // Add node IP for logging
}

var _ Connection = (*MemoryConnection)(nil)

// EventQueue implements Connection.EventQueue
func (c *MemoryConnection) EventQueue() <-chan *Message {
	return c.queue
}

// Send implements Connection.Send
func (c *MemoryConnection) Send(ctx context.Context, msg *Message) error {
	// Prepare log entry for Kafka
	if c.kafkaProducer != nil {
		logEntry := map[string]interface{}{
			"log_type":           "sse_event",
			"timestamp":          time.Now().Format(time.RFC3339),
			"startTime":          time.Now().Format(time.DateTime),
			"endTime":            time.Now().Format(time.DateTime), 
			"session_id":         c.meta.ID,
			"consumer_token":     c.meta.ConsumerToken,
			"event_type":         msg.Event,
			"event_data":         string(msg.Data),
			"method":             c.meta.Request.Headers["Method"], // Assuming Method is stored in Headers
			"path":               c.meta.Prefix,                    // Using Prefix as path for SSE events
			"query":              c.meta.Request.Query,
			"remote_addr":        c.meta.Request.Headers["X-Forwarded-For"], // Assuming X-Forwarded-For for remote_addr
			"user_agent":         c.meta.Request.Headers["User-Agent"],      // Assuming User-Agent in Headers
			"service_identifier": c.meta.Prefix,
			"node_ip":            c.nodeIP,
		}

		go func() {
			logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := c.kafkaProducer.Produce(logCtx, cnst.KafkaTopicSseEvent, c.meta.Prefix, logEntry)
			if err != nil {
				c.logger.Error("failed to send SSE event log to Kafka", zap.Error(err), zap.String("session_id", c.meta.ID))
			}
		}()
	}

	select {
	case c.queue <- msg:
		return nil
	default:
		return fmt.Errorf("message queue is full")
	}
}

// Close implements Connection.Close
func (c *MemoryConnection) Close(_ context.Context) error {
	close(c.queue)
	return nil
}

// Meta implements Connection.Meta
func (c *MemoryConnection) Meta() *Meta {
	return c.meta
}

// ErrSessionNotFound is returned when a session is not found
var ErrSessionNotFound = fmt.Errorf("session not found")
