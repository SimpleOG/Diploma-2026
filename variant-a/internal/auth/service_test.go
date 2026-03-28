package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/chat-diploma/variant-a/internal/auth"
)

const (
	testSecret = "test-secret-key-for-unit-tests"
	testUserID = "550e8400-e29b-41d4-a716-446655440000"
	testUser   = "testuser"
)

func newService(t *testing.T) *auth.Service {
	t.Helper()
	return auth.NewService(testSecret, 24)
}

// ── GenerateToken / ValidateToken ────────────────────────────────────────────

func TestGenerateToken_ReturnsNonEmptyToken(t *testing.T) {
	svc := newService(t)
	token, err := svc.GenerateToken(testUserID, testUser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	// JWT has three dot-separated parts.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}
}

func TestValidateToken_RoundTrip(t *testing.T) {
	svc := newService(t)

	token, err := svc.GenerateToken(testUserID, testUser)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	uid, uname, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if uid != testUserID {
		t.Errorf("expected userID %q, got %q", testUserID, uid)
	}
	if uname != testUser {
		t.Errorf("expected username %q, got %q", testUser, uname)
	}
}

func TestValidateToken_InvalidToken(t *testing.T) {
	svc := newService(t)

	_, _, err := svc.ValidateToken("not.a.token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	svc1 := auth.NewService("secret-1", 24)
	svc2 := auth.NewService("secret-2", 24)

	token, err := svc1.GenerateToken(testUserID, testUser)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	_, _, err = svc2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for token signed with different secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	// Create a service with -1 hour expiration so the token is immediately expired.
	svc := auth.NewService(testSecret, -1)

	token, err := svc.GenerateToken(testUserID, testUser)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// Give the token a moment to expire.
	time.Sleep(10 * time.Millisecond)

	_, _, err = svc.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateToken_EmptyString(t *testing.T) {
	svc := newService(t)
	_, _, err := svc.ValidateToken("")
	if err == nil {
		t.Fatal("expected error for empty token string")
	}
}

// ── HashPassword / CheckPassword ─────────────────────────────────────────────

func TestHashPassword_ProducesNonEmptyHash(t *testing.T) {
	svc := newService(t)
	hash, err := svc.HashPassword("mypassword")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == "mypassword" {
		t.Fatal("hash should not equal plaintext")
	}
}

func TestHashPassword_DifferentCallsDifferentHashes(t *testing.T) {
	svc := newService(t)
	h1, _ := svc.HashPassword("same-password")
	h2, _ := svc.HashPassword("same-password")
	if h1 == h2 {
		t.Fatal("bcrypt hashes for the same password should differ due to random salt")
	}
}

func TestCheckPassword_CorrectPassword(t *testing.T) {
	svc := newService(t)
	hash, err := svc.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !svc.CheckPassword(hash, "correct-horse-battery-staple") {
		t.Fatal("CheckPassword returned false for correct password")
	}
}

func TestCheckPassword_WrongPassword(t *testing.T) {
	svc := newService(t)
	hash, _ := svc.HashPassword("correct")
	if svc.CheckPassword(hash, "wrong") {
		t.Fatal("CheckPassword returned true for wrong password")
	}
}

func TestCheckPassword_EmptyPassword(t *testing.T) {
	svc := newService(t)
	hash, _ := svc.HashPassword("notempty")
	if svc.CheckPassword(hash, "") {
		t.Fatal("CheckPassword returned true for empty password")
	}
}

// ── Table-driven tests ────────────────────────────────────────────────────────

func TestGenerateAndValidateToken_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		username string
	}{
		{"simple", "user-1", "alice"},
		{"uuid", "550e8400-e29b-41d4-a716-446655440001", "bob"},
		{"unicode", "user-3", "用户名"},
		{"spaces", "user-4", "john doe"},
	}

	svc := newService(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, err := svc.GenerateToken(tc.userID, tc.username)
			if err != nil {
				t.Fatalf("GenerateToken: %v", err)
			}
			uid, uname, err := svc.ValidateToken(token)
			if err != nil {
				t.Fatalf("ValidateToken: %v", err)
			}
			if uid != tc.userID {
				t.Errorf("userID mismatch: want %q got %q", tc.userID, uid)
			}
			if uname != tc.username {
				t.Errorf("username mismatch: want %q got %q", tc.username, uname)
			}
		})
	}
}
