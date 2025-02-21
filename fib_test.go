package main

import "testing"

func TestFibonacci(t *testing.T) {
	fib := NewFib()

	expected := []int{1, 2, 3, 5, 8, 13, 21, 34, 55, 89}
	for _, value := range expected {
		result := fib.Next()
		if result != value {
			t.Errorf("Expected %d, got %d", value, result)
		}
	}
}

func TestFibonacciOverflow(t *testing.T) {
	fib := &Fib{
		value1: 2147483647, // Max int32 value
		value2: 1,
	}

	result := fib.Next()
	if result != -1 {
		t.Errorf("Expected -1 (overflow), got %d", result)
	}
}

func TestFibonacciNegative(t *testing.T) {
	fib := &Fib{
		value1: -1,
		value2: 1,
	}

	result := fib.Next()
	if result != -1 {
		t.Errorf("Expected -1 (negative value), got %d", result)
	}

	fib = &Fib{
		value1: 1,
		value2: -1,
	}

	result = fib.Next()
	if result != -1 {
		t.Errorf("Expected -1 (negative value), got %d", result)
	}
}

func TestNewFib(t *testing.T) {
	fib := NewFib()
	if fib.value1 != 0 || fib.value2 != 1 {
		t.Errorf("Expected value1 to be 0 and value2 to be 1, got value1=%d, value2=%d", fib.value1, fib.value2)
	}
}
