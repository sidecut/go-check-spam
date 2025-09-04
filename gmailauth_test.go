package main

import (
    "encoding/base64"
    "testing"
)

func TestRandomStateLength(t *testing.T) {
    cases := []int{1, 8, 16, 32, 0}
    for _, n := range cases {
        s, err := randomState(n)
        if err != nil {
            t.Fatalf("randomState(%d) returned error: %v", n, err)
        }
        if n == 0 {
            if s != "" {
                t.Fatalf("expected empty string for n=0, got %q", s)
            }
            continue
        }
        dec, err := base64.RawURLEncoding.DecodeString(s)
        if err != nil {
            t.Fatalf("failed to decode randomState(%d) result %q: %v", n, s, err)
        }
        if len(dec) != n {
            t.Fatalf("decoded length mismatch for n=%d: want %d got %d (encoded %q)", n, n, len(dec), s)
        }
    }
}

func TestRandomStateUnique(t *testing.T) {
    a, err := randomState(16)
    if err != nil {
        t.Fatalf("randomState returned error: %v", err)
    }
    b, err := randomState(16)
    if err != nil {
        t.Fatalf("randomState returned error: %v", err)
    }
    if a == b {
        t.Fatalf("randomState returned identical values twice: %q", a)
    }
}
