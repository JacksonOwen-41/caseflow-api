package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "correct horse battery staple" || !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("password was not hashed or could not be verified")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password was accepted")
	}
	for _, password := range []string{"too short", strings.Repeat("x", 73)} {
		if _, err := HashPassword(password); err != ErrInvalidPassword {
			t.Fatalf("expected invalid password error for length %d, got %v", len(password), err)
		}
	}
}

func TestTokenHash(t *testing.T) {
	token, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := HashToken(token)
	if !ok || string(got) != string(hash) {
		t.Fatal("token hash did not round trip")
	}
	if _, ok := HashToken(token + "a"); ok {
		t.Fatal("tampered token was accepted")
	}
}