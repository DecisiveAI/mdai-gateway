package stringutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpperFirst(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"h", "H"},
		{"", ""},
		{"123abc", "123abc"},
	}

	for _, tt := range tests {
		result := UpperFirst(tt.input)
		assert.Equal(t, tt.expected, result, "input: %q", tt.input)
	}
}
