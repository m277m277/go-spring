/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package textstyle_test

import (
	"testing"

	"go-spring.org/stdlib/testing/assert"
	"go-spring.org/stdlib/textstyle"
)

func TestAttribute_Sprint(t *testing.T) {
	tests := []struct {
		name     string
		attr     textstyle.Attribute
		input    string
		expected string
	}{
		{
			name:     "Bold attribute",
			attr:     textstyle.Bold,
			input:    "test",
			expected: "\x1b[1mtest\x1b[0m",
		},
		{
			name:     "Italic attribute",
			attr:     textstyle.Italic,
			input:    "test",
			expected: "\x1b[3mtest\x1b[0m",
		},
		{
			name:     "Underline attribute",
			attr:     textstyle.Underline,
			input:    "test",
			expected: "\x1b[4mtest\x1b[0m",
		},
		{
			name:     "ReverseVideo attribute",
			attr:     textstyle.ReverseVideo,
			input:    "test",
			expected: "\x1b[7mtest\x1b[0m",
		},
		{
			name:     "CrossedOut attribute",
			attr:     textstyle.CrossedOut,
			input:    "test",
			expected: "\x1b[9mtest\x1b[0m",
		},
		{
			name:     "Red color",
			attr:     textstyle.Red,
			input:    "test",
			expected: "\x1b[31mtest\x1b[0m",
		},
		{
			name:     "BgGreen background",
			attr:     textstyle.BgGreen,
			input:    "test",
			expected: "\x1b[42mtest\x1b[0m",
		},
		// Remaining foreground and background colors.
		{name: "Black", attr: textstyle.Black, input: "x", expected: "\x1b[30mx\x1b[0m"},
		{name: "Green", attr: textstyle.Green, input: "x", expected: "\x1b[32mx\x1b[0m"},
		{name: "Yellow", attr: textstyle.Yellow, input: "x", expected: "\x1b[33mx\x1b[0m"},
		{name: "Blue", attr: textstyle.Blue, input: "x", expected: "\x1b[34mx\x1b[0m"},
		{name: "Magenta", attr: textstyle.Magenta, input: "x", expected: "\x1b[35mx\x1b[0m"},
		{name: "Cyan", attr: textstyle.Cyan, input: "x", expected: "\x1b[36mx\x1b[0m"},
		{name: "White", attr: textstyle.White, input: "x", expected: "\x1b[37mx\x1b[0m"},
		{name: "BgBlack", attr: textstyle.BgBlack, input: "x", expected: "\x1b[40mx\x1b[0m"},
		{name: "BgRed", attr: textstyle.BgRed, input: "x", expected: "\x1b[41mx\x1b[0m"},
		{name: "BgYellow", attr: textstyle.BgYellow, input: "x", expected: "\x1b[43mx\x1b[0m"},
		{name: "BgBlue", attr: textstyle.BgBlue, input: "x", expected: "\x1b[44mx\x1b[0m"},
		{name: "BgMagenta", attr: textstyle.BgMagenta, input: "x", expected: "\x1b[45mx\x1b[0m"},
		{name: "BgCyan", attr: textstyle.BgCyan, input: "x", expected: "\x1b[46mx\x1b[0m"},
		{name: "BgWhite", attr: textstyle.BgWhite, input: "x", expected: "\x1b[47mx\x1b[0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.attr.Sprint(tt.input)
			assert.String(t, result).Equal(tt.expected)
		})
	}
}

func TestAttribute_Sprintf(t *testing.T) {
	result := textstyle.Red.Sprintf("hello %s", "world")
	assert.String(t, result).Equal("\x1b[31mhello world\x1b[0m")
}

func TestText_Sprint(t *testing.T) {
	// Test empty attributes
	text := textstyle.NewText()
	result := text.Sprint("test")
	assert.String(t, result).Equal("test")

	// Test multiple attributes
	attributes := []textstyle.Attribute{
		textstyle.Bold,
		textstyle.Red,
		textstyle.BgGreen,
	}
	textWithAttrs := textstyle.NewText(attributes...)
	result = textWithAttrs.Sprint("test")
	assert.String(t, result).Equal("\x1b[1;31;42mtest\x1b[0m")

	// Sprint with no args still emits the wrap codes around an empty string.
	assert.String(t, textstyle.Red.Sprint()).Equal("\x1b[31m\x1b[0m")

	// Sprint concatenates multiple args like fmt.Sprint.
	assert.String(t, textstyle.Red.Sprint("a", 1)).Equal("\x1b[31ma1\x1b[0m")
}

func TestText_Sprintf(t *testing.T) {
	attributes := []textstyle.Attribute{
		textstyle.Bold,
		textstyle.Blue,
	}
	text := textstyle.NewText(attributes...)
	result := text.Sprintf("hello %s", "world")
	assert.String(t, result).Equal("\x1b[1;34mhello world\x1b[0m")
}

func TestANSIFormatCorrectness(t *testing.T) {
	result := textstyle.Bold.Sprint("test")
	assert.String(t, result).HasPrefix("\x1b[")
	assert.String(t, result).HasSuffix("\x1b[0m")
	assert.String(t, result).Contains("test")
}
