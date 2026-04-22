package main

import (
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestReinterpretAsCP1251(t *testing.T) {
	dec := charmap.Windows1251.NewDecoder()
	tests := []struct {
		name, in, want string
	}{
		{"russian mojibake", "Ïðèâåò", "Привет"},
		{"clean utf8 passthrough", "Привет", "Привет"},
		{"ascii passthrough", "Hello", "Hello"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reinterpretAsCP1251(tt.in, dec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("reinterpretAsCP1251(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEncodingHint(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"clean english", []string{"Hello", "World"}, ""},
		{"russian mojibake", []string{"Ïðèâåò ìèð"}, "cp1251"},
		{"short accented latin (café)", []string{"Café"}, ""},
		{"empty", []string{""}, ""},
		{"already utf8 russian", []string{"Привет мир"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodingHint(tt.in...)
			if got != tt.want {
				t.Errorf("encodingHint(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}
