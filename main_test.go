package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindIndentation_IndentedIf(t *testing.T) {
	lines := []string{
		"  {{ if foo }}",
	}
	assert.Equal(t, 2, findIndentation(lines, 0))
}

func TestFindIndentation_UnIndentedIf(t *testing.T) {
	lines := []string{
		"{{ if foo }}",
	}
	assert.Equal(t, 0, findIndentation(lines, 0))
}

func TestFindIndentation_UnIndentedIf_MultilineStr(t *testing.T) {
	lines := []string{
		"   foo: |", // 3 spaces
		"# foobar",
		"  {{ if foo }}", // 2 spaces
	}
	assert.Equal(t, 5 /* 3 + 2 spaces */, findIndentation(lines, 2))
}
