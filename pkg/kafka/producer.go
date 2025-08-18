package kafka

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// ProducerConfig holds Kafka producer configuration
type ProducerConfig struct {
	Brokers []string
	Topic   string
}

// KafkaProducer is a wrapper for kafka.Writer
type KafkaProducer struct {
	writer *kafka.Writer
	logger *zap.Logger
}

// ZapKafkaLogger implements the kafka.Logger interface using zap.Logger
type ZapKafkaLogger struct {
	logger *zap.Logger
}

// Printf logs a formatted string.
func (l *ZapKafkaLogger) Printf(template string, args ...interface{}) {
	l.logger.Info(template, zap.Any("args", args))
}

// Println logs a line.
func (l *ZapKafkaLogger) Println(args ...interface{}) {
	l.logger.Info(zap.Any("args", args).String)
}

// Fatal logs a fatal error and exits.
func (l *ZapKafkaLogger) Fatal(args ...interface{}) {
	l.logger.Fatal(zap.Any("args", args).String)
}

// Panic logs a panic error and panics.
func (l *ZapKafkaLogger) Panic(args ...interface{}) {
	l.logger.Panic(zap.Any("args", args).String)
}

// NewKafkaProducer creates a new KafkaProducer
func NewKafkaProducer(cfg *ProducerConfig, logger *zap.Logger) *KafkaProducer {
	zapLogger := logger.With(zap.String("component", "kafka-writer"))
	writer := &kafka.Writer{
		Addr:     kafka.TCP(cfg.Brokers...),
		Topic:    cfg.Topic,
		Balancer: &kafka.LeastBytes{},
		Logger:   &ZapKafkaLogger{logger: zapLogger}, // Use the custom ZapKafkaLogger
	}
	return &KafkaProducer{
		writer: writer,
		logger: logger,
	}
}

// Produce sends a message to Kafka
func (kp *KafkaProducer) Produce(ctx context.Context, key string, value interface{}) error {
	messageValue, err := json.Marshal(value)
	if err != nil {
		kp.logger.Error("failed to marshal message value", zap.Error(err))
		return err
	}

	msg := kafka.Message{
		Key:   []byte(key),
		Value: messageValue,
		Time:  time.Now(),
	}

	err = kp.writer.WriteMessages(ctx, msg)
	if err != nil {
		kp.logger.Error("failed to write message to kafka", zap.Error(err))
		return err
	}
	kp.logger.Debug("message sent to kafka", zap.String("key", key), zap.ByteString("value", messageValue))
	return nil
}

// Close closes the Kafka producer
func (kp *KafkaProducer) Close() error {
	return kp.writer.Close()
}
