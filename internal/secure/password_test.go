package secure

import "testing"

func TestPasswordHasher(t *testing.T) {
	t.Parallel()

	hasher := NewPasswordHasher(PasswordParams{
		MemoryKiB:   8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if encoded == "correct horse battery staple" {
		t.Fatal("Hash() returned plaintext")
	}

	match, err := hasher.Verify(encoded, "correct horse battery staple")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !match {
		t.Fatal("Verify() = false for the correct password")
	}

	match, err = hasher.Verify(encoded, "wrong password")
	if err != nil {
		t.Fatalf("Verify() wrong password error = %v", err)
	}
	if match {
		t.Fatal("Verify() = true for a wrong password")
	}
}

func TestPasswordHasherRejectsMalformedHash(t *testing.T) {
	t.Parallel()

	hasher := NewPasswordHasher(DefaultPasswordParams())
	if _, err := hasher.Verify("$argon2id$broken", "password"); err == nil {
		t.Fatal("Verify() accepted a malformed hash")
	}
}
