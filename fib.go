package main

type Fib struct {
	value1 int
	value2 int
}

// NewFib returns a new Fib struct.
func NewFib() *Fib {
	return &Fib{
		value1: 0,
		value2: 1,
	}
}

// Next returns the next Fibonacci number.
func (f *Fib) Next() int {
	return f.next()
}

// next calculates the next Fibonacci number.
func (f *Fib) next() int {
	if f.value1 < 0 || f.value2 < 0 {
		return -1
	}

	// Calculate the next Fibonacci number
	next := f.value1 + f.value2

	// Check for overflow
	if next < 0 {
		return -1
	}

	// Update the internal state
	f.value1 = f.value2
	f.value2 = next

	return next
}
