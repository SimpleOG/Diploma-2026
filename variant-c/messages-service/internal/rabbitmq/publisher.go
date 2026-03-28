package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	exchangeName = "messaging.events"
	exchangeType = "topic"
)

// Publisher manages a RabbitMQ connection and publishes messages to an exchange.
type Publisher struct {
	url        string
	conn       *amqp.Connection
	ch         *amqp.Channel
	mu         sync.Mutex
	confirms   chan amqp.Confirmation
	closeChan  chan *amqp.Error
}

// NewPublisher creates a new Publisher and establishes a connection.
func NewPublisher(url string) (*Publisher, error) {
	p := &Publisher{url: url}
	if err := p.connect(); err != nil {
		return nil, err
	}
	go p.watchConnection()
	return p, nil
}

// connect establishes connection, channel, declares exchange, and enables publisher confirms.
func (p *Publisher) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("connect to rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("open channel: %w", err)
	}

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

	if err := ch.Confirm(false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("enable publisher confirms: %w", err)
	}

	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 128))
	closeChan := make(chan *amqp.Error, 1)
	conn.NotifyClose(closeChan)

	p.mu.Lock()
	p.conn = conn
	p.ch = ch
	p.confirms = confirms
	p.closeChan = closeChan
	p.mu.Unlock()

	slog.Info("connected to RabbitMQ (publisher)")
	return nil
}

// watchConnection monitors the connection and reconnects on failure.
func (p *Publisher) watchConnection() {
	for {
		p.mu.Lock()
		closeChan := p.closeChan
		p.mu.Unlock()

		if closeChan == nil {
			return
		}

		amqpErr, ok := <-closeChan
		if !ok {
			return
		}
		if amqpErr != nil {
			slog.Warn("rabbitmq publisher connection closed, reconnecting",
				"reason", amqpErr.Reason)
		}

		for i := 0; i < 10; i++ {
			time.Sleep(time.Duration(i+1) * time.Second)
			if err := p.connect(); err != nil {
				slog.Warn("failed to reconnect to rabbitmq (publisher)",
					"attempt", i+1, "error", err)
				continue
			}
			break
		}
	}
}

// Publish sends a message to the exchange with the given routing key.
// It waits for a publisher confirm before returning.
func (p *Publisher) Publish(ctx context.Context, routingKey string, body []byte) error {
	p.mu.Lock()
	ch := p.ch
	confirms := p.confirms
	p.mu.Unlock()

	if ch == nil {
		return fmt.Errorf("rabbitmq channel not available")
	}

	err := ch.PublishWithContext(ctx,
		exchangeName,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
			Timestamp:    time.Now(),
		},
	)
	if err != nil {
		return fmt.Errorf("publish message: %w", err)
	}

	// Wait for publisher confirm.
	select {
	case confirm, ok := <-confirms:
		if !ok {
			return fmt.Errorf("publisher confirm channel closed")
		}
		if !confirm.Ack {
			return fmt.Errorf("message was nacked by broker")
		}
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for confirm: %w", ctx.Err())
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for publisher confirm")
	}

	return nil
}

// Close shuts down the publisher gracefully.
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}
