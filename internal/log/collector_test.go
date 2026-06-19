package log

import (
	"io"
	"strings"
	"sync"
	"testing"
)

func TestNewCollector_InitialState(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector() should not return nil")
	}
	if s := c.String(); s != "" {
		t.Errorf("expected empty string, got %q", s)
	}
	if lines := c.Lines(); len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestCollector_Collect(t *testing.T) {
	c := NewCollector()
	input := "line one\nline two\nline three\n"

	result, err := c.Collect(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "line one\nline two\nline three" {
		t.Errorf("unexpected output: %q", result)
	}
	if s := c.String(); s != "line one\nline two\nline three" {
		t.Errorf("String() mismatch: %q", s)
	}
}

func TestCollector_CollectLines(t *testing.T) {
	c := NewCollector()
	input := "alpha\nbeta\ngamma"

	lines, err := c.CollectLines(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"alpha", "beta", "gamma"}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %v", len(expected), len(lines), lines)
	}
	for i, line := range lines {
		if line != expected[i] {
			t.Errorf("line %d: expected %q, got %q", i, expected[i], line)
		}
	}
}

func TestCollector_CollectEmpty(t *testing.T) {
	c := NewCollector()

	result, err := c.Collect(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestCollector_Reset(t *testing.T) {
	c := NewCollector()
	_, _ = c.Collect(strings.NewReader("some data"))

	c.Reset()
	if s := c.String(); s != "" {
		t.Errorf("expected empty after reset, got %q", s)
	}
	if lines := c.Lines(); len(lines) != 0 {
		t.Errorf("expected 0 lines after reset, got %d", len(lines))
	}

	// Should still work after reset
	_, _ = c.Collect(strings.NewReader("after reset"))
	if s := c.String(); s != "after reset" {
		t.Errorf("expected 'after reset', got %q", s)
	}
}

func TestCollector_StringTrailingNewline(t *testing.T) {
	c := NewCollector()
	_, _ = c.Collect(strings.NewReader("line1\nline2\n"))

	s := c.String()
	if s != "line1\nline2" {
		t.Errorf("expected trailing newline trimmed, got %q", s)
	}
}

func TestCollector_Callback(t *testing.T) {
	c := NewCollector()

	var mu sync.Mutex
	received := make(map[string]int)
	var wg sync.WaitGroup

	c.SetCallback(func(line string) {
		mu.Lock()
		received[line]++
		mu.Unlock()
		wg.Done()
	})

	input := "a\nb\nc\n"
	wg.Add(3)

	_, err := c.Collect(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Fatalf("expected 3 unique callback values, got %d: %v", len(received), received)
	}
	// Each line should have been received exactly once
	for _, line := range []string{"a", "b", "c"} {
		if received[line] != 1 {
			t.Errorf("expected line %q called exactly once, got %d", line, received[line])
		}
	}
}

func TestCollector_CallbackPanic(t *testing.T) {
	c := NewCollector()

	var mu sync.Mutex
	var completed []string
	var wg sync.WaitGroup

	c.SetCallback(func(line string) {
		// Panic for any line containing "panic"
		if strings.Contains(line, "panic") {
			panic("simulated callback panic")
		}
		// Non-panicking callbacks record and signal
		mu.Lock()
		completed = append(completed, line)
		mu.Unlock()
		wg.Done()
	})

	input := "first\npanic-here\nthird\n"
	wg.Add(2) // first and third lines are non-panicking

	_, err := c.Collect(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wg.Wait()

	// All 3 lines should still be collected despite the panic
	lines := c.Lines()
	if len(lines) != 3 {
		t.Errorf("expected 3 lines collected, got %d: %v", len(lines), lines)
	}

	// Non-panicking callbacks should have both been invoked
	mu.Lock()
	if len(completed) != 2 {
		t.Errorf("expected 2 completed callbacks, got %d: %v", len(completed), completed)
	}
	mu.Unlock()
}

func TestCollector_SetCallbackNil(t *testing.T) {
	c := NewCollector()
	c.SetCallback(nil) // should not panic
	_, err := c.Collect(strings.NewReader("some data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCollector_ConcurrentSafety(t *testing.T) {
	c := NewCollector()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			input := strings.Repeat("line from goroutine X\n", 5)
			_, err := c.Collect(strings.NewReader(input))
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	// Should have collected all lines (10 goroutines * 5 lines)
	lines := c.Lines()
	expectedLines := numGoroutines * 5
	if len(lines) != expectedLines {
		t.Errorf("expected %d lines, got %d", expectedLines, len(lines))
	}
}

func TestCollector_LinesReturnsCopy(t *testing.T) {
	c := NewCollector()
	_, _ = c.Collect(strings.NewReader("one\ntwo"))

	lines := c.Lines()
	// Modify the returned slice
	for i := range lines {
		lines[i] = "modified"
	}

	// Original should be unchanged
	original := c.Lines()
	if original[0] == "modified" {
		t.Error("Lines() should return a copy of internal data")
	}
}

// ─── MultiReader Tests ─────────────────────────────────────

func TestMultiReader_Merge(t *testing.T) {
	r1 := strings.NewReader("hello ")
	r2 := strings.NewReader("world")

	mr := NewMultiReader(r1, r2)
	data, err := io.ReadAll(mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MultiReader alternates between readers, so output depends on buffer sizing
	// We just check that all content from both readers appears, order unspecified
	output := string(data)
	if !strings.Contains(output, "hello") {
		t.Errorf("expected 'hello' in output, got %q", output)
	}
	if !strings.Contains(output, "world") {
		t.Errorf("expected 'world' in output, got %q", output)
	}
}

func TestMultiReader_Empty(t *testing.T) {
	mr := NewMultiReader()
	data, err := io.ReadAll(mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty output, got %q", string(data))
	}
}

func TestMultiReader_SingleReader(t *testing.T) {
	mr := NewMultiReader(strings.NewReader("just one"))
	data, err := io.ReadAll(mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "just one" {
		t.Errorf("expected 'just one', got %q", string(data))
	}
}

func TestMultiReader_NilReader(t *testing.T) {
	mr := NewMultiReader(nil, strings.NewReader("works"), nil)
	data, err := io.ReadAll(mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "works" {
		t.Errorf("expected 'works', got %q", string(data))
	}
}

func TestMultiReader_AllNils(t *testing.T) {
	mr := NewMultiReader(nil, nil)
	data, err := io.ReadAll(mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty output, got %q", string(data))
	}
}
