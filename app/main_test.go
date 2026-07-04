package main

import (
	"testing"
	"time"
)

func TestIncrExistingInteger(t *testing.T) {
	db := &SafeDB{data: make(map[string]entry)}

	// Arrange: a key that already exists and holds a numeric string.
	db.SET("counter", "5", time.Time{})

	// Act
	got, ok := db.INCR("counter")

	// Assert: it reported the key as present
	if !ok {
		t.Errorf("INCR reported ok=false for an existing key")
	}
	// Assert: the returned value is the incremented number
	if got != 6 {
		t.Errorf("INCR returned %d, want 6", got)
	}
	// Assert: the stored value was actually updated on disk
	if v := db.data["counter"].value; v != "6" {
		t.Errorf("stored value = %q, want \"6\"", v)
	}
}
