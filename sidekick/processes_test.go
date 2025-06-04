package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestFilterOutputEmptyInput tests that filters don't hang when given empty input
func TestFilterOutputEmptyInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		filters  [][]string
		expected string
		timeout  time.Duration
	}{
		{
			name:     "empty input to grep",
			input:    "",
			filters:  [][]string{{"grep", "pattern"}},
			expected: "",
			timeout:  2 * time.Second,
		},
		{
			name:     "empty input to grep then wc",
			input:    "",
			filters:  [][]string{{"grep", "pattern"}, {"wc", "-l"}},
			expected: "0\n",
			timeout:  2 * time.Second,
		},
		{
			name:     "normal input with no matches",
			input:    "hello world\ntest line",
			filters:  [][]string{{"grep", "nonexistent"}},
			expected: "",
			timeout:  2 * time.Second,
		},
		{
			name:     "normal input with matches",
			input:    "hello world\ntest line",
			filters:  [][]string{{"grep", "test"}},
			expected: "test line\n",
			timeout:  2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			
			// Run with timeout to catch hanging
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()
			
			done := make(chan struct{})
			var result string
			var err error
			
			go func() {
				result, err = filterOutput(tt.input, tt.filters)
				close(done)
			}()
			
			select {
			case <-done:
				elapsed := time.Since(start)
				t.Logf("Test completed in %v", elapsed)
				
				if elapsed > 1*time.Second {
					t.Errorf("Test took too long: %v (should be < 1s)", elapsed)
				}
				
				if err != nil {
					t.Errorf("filterOutput failed: %v", err)
				}
				
				// Trim whitespace for comparison as wc output may vary
				result = strings.TrimSpace(result)
				expected := strings.TrimSpace(tt.expected)
				
				if result != expected {
					t.Errorf("got %q, want %q", result, expected)
				}
				
			case <-ctx.Done():
				t.Fatalf("Test timed out after %v - filter is hanging!", tt.timeout)
			}
		})
	}
}

// TestEmptyInputDoesNotHang specifically tests the hanging issue
func TestEmptyInputDoesNotHang(t *testing.T) {
	// This should complete almost instantly
	timeout := time.After(1 * time.Second)
	done := make(chan bool)
	
	go func() {
		_, err := filterOutput("", [][]string{{"grep", "test"}})
		if err != nil {
			t.Logf("filterOutput returned error (expected): %v", err)
		}
		done <- true
	}()
	
	select {
	case <-done:
		t.Log("✓ Empty input test completed without hanging")
	case <-timeout:
		t.Fatal("✗ Empty input test hung for more than 1 second!")
	}
}

// TestFilterOutputTiming ensures filters complete quickly even with empty input
func TestFilterOutputTiming(t *testing.T) {
	// Test that grep with empty input completes quickly
	start := time.Now()
	
	// Create a context with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	
	// Run filterOutput in a goroutine
	done := make(chan struct{})
	
	go func() {
		// Simulate what happens in actual usage
		_, _ = filterOutput("", [][]string{{"grep", "-E", "error|warning"}})
		close(done)
	}()
	
	select {
	case <-done:
		elapsed := time.Since(start)
		t.Logf("Filter completed in %v", elapsed)
		if elapsed > 100*time.Millisecond {
			t.Errorf("Filter took too long with empty input: %v", elapsed)
		}
	case <-ctx.Done():
		t.Fatal("Filter timed out - grep is hanging on empty input!")
	}
}