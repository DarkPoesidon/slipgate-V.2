package config

import "testing"

func TestValidateUsername(t *testing.T) {
	valid := []string{"user1", "_service", "test-user", "test_user"}
	for _, username := range valid {
		if err := ValidateUsername(username); err != nil {
			t.Fatalf("ValidateUsername(%q) returned error: %v", username, err)
		}
	}

	invalid := []string{"", "2", "User", "two words", "user@example.com"}
	for _, username := range invalid {
		if err := ValidateUsername(username); err == nil {
			t.Fatalf("ValidateUsername(%q) unexpectedly succeeded", username)
		}
	}
}
