package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery staple", h) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong", h) || VerifyPassword("", h) {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("x", "not-a-hash") || VerifyPassword("x", "$argon2id$v=19$m=1,t=1,p=1$$") {
		t.Fatal("malformed hash accepted")
	}
	h2, _ := HashPassword("correct horse battery staple")
	if h == h2 {
		t.Fatal("salts must differ")
	}
}

func TestCodeMatches(t *testing.T) {
	s := &Service{Pepper: []byte("pepper")}
	id := [16]byte{1}
	stored := s.HashCode(id, "123456")
	if !s.CodeMatches(id, "123456", stored) || s.CodeMatches(id, "123457", stored) {
		t.Fatal("code comparison wrong")
	}
	if s.CodeMatches([16]byte{2}, "123456", stored) {
		t.Fatal("a code must be bound to its own otp id")
	}
}
