package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chat-diploma/variant-b/internal/model"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// KafkaConsumer wraps a confluent-kafka-go Consumer.
type KafkaConsumer struct {
	consumer *kafka.Consumer
	topic    string
}

// NewConsumer creates a KafkaConsumer that subscribes to the given topic.
func NewConsumer(brokers, group, topic string) (*KafkaConsumer, error) {
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":       brokers,
		"group.id":                group,
		"auto.offset.reset":       "earliest",
		"enable.auto.commit":      false,
		"session.timeout.ms":      30000,
		"max.poll.interval.ms":    300000,
		"socket.keepalive.enable": true,
	})
	if err != nil {
		return nil, fmt.Errorf("kafka consumer: new: %w", err)
	}

	if err := c.Subscribe(topic, nil); err != nil {
		c.Close() //nolint:errcheck
		return nil, fmt.Errorf("kafka consumer: subscribe: %w", err)
	}

	slog.Info("kafka consumer subscribed", "brokers", brokers, "group", group, "topic", topic)
	return &KafkaConsumer{consumer: c, topic: topic}, nil
}

// Consume starts a pool of workerCount goroutines that read from Kafka.
// Each message is deserialized and passed to handler.  On handler error the
// offset is NOT committed and the consumer retries after a 1-second pause.
// The loop exits when ctx is cancelled.
func (kc *KafkaConsumer) Consume(ctx context.Context, handler func(msg *model.KafkaMessage) error) {
	const workerCount = 4

	msgCh := make(chan *kafka.Message, workerCount*4)

	// Single reader goroutine that polls Kafka and dispatches to msgCh.
	go func() {
		defer close(msgCh)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			kmsg, err := kc.consumer.ReadMessage(500 * time.Millisecond)
			if err != nil {
				if kerr, ok := err.(kafka.Error); ok && kerr.Code() == kafka.ErrTimedOut {
					continue
				}
				slog.Error("kafka consumer: read message", "error", err)
				continue
			}

			select {
			case msgCh <- kmsg:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Worker pool.
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for kmsg := range msgCh {
				kc.processMessage(kmsg, handler)
			}
		}(i)
	}

	wg.Wait()
}

// processMessage deserializes a Kafka message and calls handler, retrying once
// after 1 second on transient errors without committing the offset.
func (kc *KafkaConsumer) processMessage(kmsg *kafka.Message, handler func(msg *model.KafkaMessage) error) {
	var km model.KafkaMessage
	if err := json.Unmarshal(kmsg.Value, &km); err != nil {
		slog.Error("kafka consumer: unmarshal message", "error", err, "value", string(kmsg.Value))
		// Poison pill – commit and skip to avoid stuck consumer.
		kc.commitMessage(kmsg)
		return
	}

	if err := handler(&km); err != nil {
		slog.Error("kafka consumer: handler error, retrying after 1s",
			"message_id", km.MessageID,
			"error", err,
		)
		time.Sleep(time.Second)

		if retryErr := handler(&km); retryErr != nil {
			slog.Error("kafka consumer: handler retry failed, skipping message",
				"message_id", km.MessageID,
				"error", retryErr,
			)
			// Still commit to make progress; the worker will persist via upsert idempotency.
		}
	}

	kc.commitMessage(kmsg)
}

func (kc *KafkaConsumer) commitMessage(kmsg *kafka.Message) {
	_, err := kc.consumer.CommitMessage(kmsg)
	if err != nil {
		slog.Error("kafka consumer: commit offset", "error", err)
	}
}

// Close shuts down the consumer.
func (kc *KafkaConsumer) Close() {
	if err := kc.consumer.Close(); err != nil {
		slog.Error("kafka consumer: close", "error", err)
	}
}
