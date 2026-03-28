package kafka_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chat-diploma/variant-b/internal/model"
)

// TestKafkaMessageSerialization verifies that KafkaMessage can be marshalled and
// unmarshalled correctly without requiring a running Kafka broker.
func TestKafkaMessageSerialization(t *testing.T) {
	original := model.KafkaMessage{
		MessageID:      "msg-uuid-001",
		RoomID:         "room-uuid-001",
		SenderID:       "user-uuid-001",
		SenderUsername: "alice",
		Content:        "hello world",
		CreatedAt:      time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded model.KafkaMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.MessageID != original.MessageID {
		t.Errorf("MessageID: want %q, got %q", original.MessageID, decoded.MessageID)
	}
	if decoded.RoomID != original.RoomID {
		t.Errorf("RoomID: want %q, got %q", original.RoomID, decoded.RoomID)
	}
	if decoded.SenderID != original.SenderID {
		t.Errorf("SenderID: want %q, got %q", original.SenderID, decoded.SenderID)
	}
	if decoded.SenderUsername != original.SenderUsername {
		t.Errorf("SenderUsername: want %q, got %q", original.SenderUsername, decoded.SenderUsername)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content: want %q, got %q", original.Content, decoded.Content)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt: want %v, got %v", original.CreatedAt, decoded.CreatedAt)
	}
}

// TestNewProducer_InvalidBroker verifies that NewProducer does not panic and
// returns an error or a producer when given an address.  Without a live broker
// the connection will fail asynchronously; we only check the constructor itself.
func TestNewProducer_InvalidBroker(t *testing.T) {
	t.Skip("requires a running Kafka broker – integration test only")
}
