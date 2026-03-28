package auth_test

import (
	"testing"
	"time"

	"github.com/chat-diploma/variant-b/internal/auth"
)

func TestGenerateAndValidateToken(t *testing.T) {
	svc := auth.NewService("test-secret-key", 24)

	token, err := svc.GenerateToken("user-123", "alice")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected UserID=user-123, got %s", claims.UserID)
	}
	if claims.Username != "alice" {
		t.Errorf("expected Username=alice, got %s", claims.Username)
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	svc1 := auth.NewService("secret-a", 24)
	svc2 := auth.NewService("secret-b", 24)

	token, err := svc1.GenerateToken("user-1", "bob")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	_, err = svc2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error when validating token signed with different secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	svc := auth.NewService("test-secret", -1) // -1 hours -> already expired
	token, err := svc.GenerateToken("user-1", "carol")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// Give the token a moment to expire.
	time.Sleep(10 * time.Millisecond)

	_, err = svc.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	plain := "supersecret"
	hash, err := auth.HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == plain {
		t.Fatal("hash should not equal plain text")
	}

	if err := auth.CheckPassword(hash, plain); err != nil {
		t.Errorf("CheckPassword with correct password: %v", err)
	}

	if err := auth.CheckPassword(hash, "wrongpass"); err == nil {
		t.Error("expected error for wrong password")
	}
}
