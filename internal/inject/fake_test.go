package inject

import (
	"context"
	"testing"
)

func TestFakeInjectorReturnsInsertedReceipt(t *testing.T) {
	injector := NewFakeInjector()

	receipt, err := injector.Insert(context.Background(), "你好，世界")
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if got, want := receipt.Target, "fake"; got != want {
		t.Fatalf("Target = %q, want %q", got, want)
	}
	if got, want := receipt.Status, "inserted"; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
}
