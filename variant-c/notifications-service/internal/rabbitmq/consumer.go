package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/chat-diploma/variant-c/notifications-service/internal/model"
)

const (
	exchangeName    = "messaging.events"
	exchangeType    = "topic"
	queueName       = "notifications.websocket"
	bindingKey      = "room.#"
	prefetchCount   = 20
)

// Broadcaster is implemented by the WebSocket hub.
type Broadcaster interface {
	BroadcastToRoom(roomID string, payload []byte)
}

// Consumer manages a RabbitMQ connection and consumes messages from the queue.
type Consumer struct {
	url         string
	broadcaster Broadcaster
	conn        *amqp.Connection
	ch          *amqp.Channel
	mu          sync.Mutex
	closeChan   chan *amqp.Error
	stopChan    chan struct{}
}

// NewConsumer creates a new Consumer.
func NewConsumer(url string, broadcaster Broadcaster) (*Consumer, error) {
	c := &Consumer{
		url:         url,
		broadcaster: broadcaster,
		stopChan:    make(chan struct{}),
	}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// connect establishes a connection, channel, declares exchange and queue.
func (c *Consumer) connect() error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("connect to rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("open channel: %w", err)
	}

	// Declare exchange.
	if err := ch.ExchangeDeclare(
		exchangeName,
		exchangeType,
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("declare exchange: %w", err)
	}

	// Declare queue.
	if _, err := ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("declare queue: %w", err)
	}

	// Bind queue to exchange.
	if err := ch.QueueBind(queueName, bindingKey, exchangeName, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("bind queue: %w", err)
	}

	// Set QoS / prefetch.
	if err := ch.Qos(prefetchCount, 0, false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("set qos: %w", err)
	}

	closeChan := make(chan *amqp.Error, 1)
	conn.NotifyClose(closeChan)

	c.mu.Lock()
	c.conn = conn
	c.ch = ch
	c.closeChan = closeChan
	c.mu.Unlock()

	slog.Info("connected to RabbitMQ (consumer)")
	return nil
}

// Start begins consuming messages. Runs in a goroutine.
func (c *Consumer) Start(ctx context.Context) {
	for {
		if err := c.consume(ctx); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-c.stopChan:
				return
			default:
				slog.Warn("consumer stopped, attempting reconnect", "error", err)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		default:
		}

		// Reconnect loop.
		for i := 0; i < 10; i++ {
			select {
			case <-ctx.Done():
				return
			case <-c.stopChan:
				return
			default:
			}

			time.Sleep(time.Duration(i+1) * time.Second)
			if err := c.connect(); err != nil {
				slog.Warn("failed to reconnect to rabbitmq (consumer)",
					"attempt", i+1, "error", err)
				continue
			}
			break
		}
	}
}

// consume sets up message delivery and processes messages.
func (c *Consumer) consume(ctx context.Context) error {
	c.mu.Lock()
	ch := c.ch
	closeChan := c.closeChan
	c.mu.Unlock()

	if ch == nil {
		return fmt.Errorf("channel not available")
	}

	deliveries, err := ch.Consume(
		queueName,
		"",    // consumer tag
		false, // auto-ack (we manual-ack)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("start consuming: %w", err)
	}

	slog.Info("started consuming from RabbitMQ queue", "queue", queueName)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.stopChan:
			return nil
		case amqpErr, ok := <-closeChan:
			if !ok || amqpErr != nil {
				if amqpErr != nil {
					return fmt.Errorf("connection closed: %s", amqpErr.Reason)
				}
				return fmt.Errorf("connection closed")
			}
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("delivery channel closed")
			}
			c.processDelivery(delivery)
		}
	}
}

// processDelivery deserializes and handles a single RabbitMQ delivery.
func (c *Consumer) processDelivery(delivery amqp.Delivery) {
	var event model.MessageEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		slog.Warn("failed to deserialize message event", "error", err)
		_ = delivery.Nack(false, false) // discard malformed message
		return
	}

	if event.RoomID == "" {
		slog.Warn("message event missing room_id")
		_ = delivery.Nack(false, false)
		return
	}

	// Build WebSocket outgoing payload.
	outgoing := map[string]interface{}{
		"type":            "new_message",
		"message_id":      event.MessageID,
		"room_id":         event.RoomID,
		"sender_id":       event.SenderID,
		"sender_username": event.SenderUsername,
		"content":         event.Content,
		"created_at":      event.CreatedAt,
	}

	payload, err := json.Marshal(outgoing)
	if err != nil {
		slog.Error("failed to marshal ws payload", "error", err)
		_ = delivery.Nack(false, false)
		return
	}

	c.broadcaster.BroadcastToRoom(event.RoomID, payload)

	if err := delivery.Ack(false); err != nil {
		slog.Error("failed to ack delivery", "error", err)
	}
}

// Close shuts down the consumer gracefully.
func (c *Consumer) Close() {
	close(c.stopChan)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ch != nil {
		_ = c.ch.Close()
		c.ch = nil
	}
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}
