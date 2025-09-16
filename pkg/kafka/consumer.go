package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// ConsumerConfig holds Kafka consumer configuration
type ConsumerConfig struct {
	Brokers []string
	Topic   string
	GroupID string
}

// KafkaConsumer is a wrapper for kafka.Reader
type KafkaConsumer struct {
	reader *kafka.Reader
	logger *zap.Logger
}

// NewKafkaConsumer creates a new KafkaConsumer
func NewKafkaConsumer(cfg *ConsumerConfig, logger *zap.Logger) (*KafkaConsumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers not provided")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("kafka topic not provided")
	}
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("kafka consumer group ID not provided")
	}

	zapLogger := logger.With(zap.String("component", "kafka-reader"), zap.String("topic", cfg.Topic), zap.String("group_id", cfg.GroupID))
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        cfg.Brokers,
		Topic:          cfg.Topic,
		GroupID:        cfg.GroupID,
		MinBytes:       10e3, // 10KB
		MaxBytes:       10e6, // 10MB
		MaxWait:        time.Second,
		CommitInterval: time.Second, // Flushes commits to Kafka every second
		Logger:         &ZapKafkaLogger{logger: zapLogger},
		ErrorLogger:    &ZapKafkaLogger{logger: zapLogger.Named("error")},
	})

	return &KafkaConsumer{
		reader: reader,
		logger: logger,
	}, nil
}

// ReadMessage reads a single message from Kafka
func (kc *KafkaConsumer) ReadMessage(ctx context.Context) (kafka.Message, error) {
	msg, err := kc.reader.ReadMessage(ctx)
	if err != nil {
		kc.logger.Error("failed to read message from kafka", zap.Error(err))
		return kafka.Message{}, err
	}
	kc.logger.Debug("message read from kafka", zap.String("topic", msg.Topic), zap.ByteString("key", msg.Key), zap.ByteString("value", msg.Value))
	return msg, nil
}

// Close closes the Kafka consumer
func (kc *KafkaConsumer) Close() error {
	return kc.reader.Close()
}
