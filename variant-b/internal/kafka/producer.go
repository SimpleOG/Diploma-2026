package kafka

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// KafkaProducer wraps a confluent-kafka-go Producer for fire-and-forget publishing.
type KafkaProducer struct {
	producer *kafka.Producer
}

// NewProducer creates a KafkaProducer connected to the given comma-separated brokers.
func NewProducer(brokers string) (*KafkaProducer, error) {
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":            brokers,
		"acks":                         "1",
		"retries":                      3,
		"retry.backoff.ms":             200,
		"socket.keepalive.enable":      true,
		"message.timeout.ms":           10000,
	})
	if err != nil {
		return nil, fmt.Errorf("kafka producer: new: %w", err)
	}

	// Background goroutine drains the delivery report channel so it never blocks.
	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					slog.Error("kafka delivery failure",
						"topic", *ev.TopicPartition.Topic,
						"error", ev.TopicPartition.Error,
					)
				}
			case kafka.Error:
				slog.Warn("kafka producer event", "code", ev.Code(), "error", ev)
			}
		}
	}()

	return &KafkaProducer{producer: p}, nil
}

// Publish asynchronously sends a message to topic with the given key and value.
// It is fire-and-forget; delivery errors are logged via the background goroutine.
func (kp *KafkaProducer) Publish(topic, key string, value []byte) error {
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &topic,
			Partition: kafka.PartitionAny,
		},
		Key:       []byte(key),
		Value:     value,
		Timestamp: time.Now().UTC(),
	}

	if err := kp.producer.Produce(msg, nil); err != nil {
		return fmt.Errorf("kafka producer: produce: %w", err)
	}
	return nil
}

// Close flushes in-flight messages and shuts down the producer.
func (kp *KafkaProducer) Close() {
	remaining := kp.producer.Flush(5000)
	if remaining > 0 {
		slog.Warn("kafka producer: flushed with outstanding messages", "remaining", remaining)
	}
	kp.producer.Close()
}
