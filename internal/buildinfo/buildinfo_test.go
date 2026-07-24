package buildinfo

import "testing"

func TestString(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	t.Cleanup(func() {
		Version, Commit = originalVersion, originalCommit
	})

	Version = "1.2.3"
	Commit = "0123456789abcdef"

	if got, want := String("k8s-panel"), "k8s-panel 1.2.3 (commit 0123456789abcdef)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
