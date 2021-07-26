package hello

import "testing"

func HelloTest(t *testing.T) {
	want := "hello golang!"
	if got := Hello(); got != want {
		t.Errorf("Hello() = %q, want %q", got, want)
	}
}
