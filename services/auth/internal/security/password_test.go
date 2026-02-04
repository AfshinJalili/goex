package security

import "testing"

func TestPasswordHashVerify(t *testing.T) {
	params := Argon2Params{Memory: 64 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := HashPassword("s3cret", params)
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}

	ok, err := VerifyPassword("s3cret", hash)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !ok {
		t.Fatalf("expected password to verify")
	}

	ok, err = VerifyPassword("wrong", hash)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if ok {
		t.Fatalf("expected password to fail")
	}
}
