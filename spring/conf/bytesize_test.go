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

package conf_test

import (
	"math"
	"strconv"
	"testing"

	"go-spring.org/spring/conf"
	"go-spring.org/stdlib/flatten"
	"go-spring.org/stdlib/testing/assert"
)

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in   string
		want conf.ByteSize
	}{
		{"0", 0},
		{"1024", 1024},
		{"1B", 1},
		{"1KB", 1 << 10},
		{"1kb", 1 << 10},
		{"1K", 1 << 10},
		{"1KiB", 1 << 10},
		{"1MB", 1 << 20},
		{"1MiB", 1 << 20},
		{"1M", 1 << 20},
		{"1GB", 1 << 30},
		{"1TB", 1 << 40},
		{"1.5KB", 1536},
		{"1.5MB", 1.5 * (1 << 20)},
		{"  16MB  ", 16 << 20},
		{"2.5GiB", 2.5 * (1 << 30)},
		{"1 KB", 1 << 10}, // whitespace between number and suffix
		{"+5MB", 5 << 20}, // explicit plus sign
		{".5KB", 512},     // leading dot
		{"1023B", 1023},   // stays in bytes
		{"8TiB", 8 << 40}, // largest suffix
		{"1.999B", 2},     // fractional bytes round to nearest
		{"0.4B", 0},       // ... and down to zero
		// 2^63-1024 is the largest multiple of 1024 a float64 represents
		// exactly below the int64 ceiling; anything finer rounds up to 2^63.
		{strconv.FormatInt(math.MaxInt64-1023, 10), conf.ByteSize(9223372036854774784)},
	}
	for _, c := range cases {
		got, err := conf.ParseByteSize(c.in)
		assert.Error(t, err).Nil()
		assert.That(t, got).Equal(c.want)
		assert.That(t, got.Bytes()).Equal(int64(c.want))
	}
}

func TestParseByteSize_Errors(t *testing.T) {
	bad := []struct {
		in   string
		want string
	}{
		{"", "empty byte size"},
		{"   ", "empty byte size"},
		{"KB", "missing number"},
		{"1.5.3", "invalid syntax"},
		{"-1MB", "negative byte size"},
		{"1QB", "unknown suffix"},
		{"1e3", "unknown suffix"},
		{"9223372036854775807", "overflows int64"}, // ParseFloat rounds to 2^63
		{"9223372036854775808", "overflows int64"}, // exactly 2^63
		{"10000000TiB", "overflows int64"},         // ~2^63.25
	}
	for _, c := range bad {
		_, err := conf.ParseByteSize(c.in)
		assert.Error(t, err).Matches(c.want)
	}
}

func TestByteSizeString(t *testing.T) {
	cases := []struct {
		in   conf.ByteSize
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{1 << 20, "1.0MiB"},
		{(1 << 30) * 3, "3.0GiB"},
		{-3, "-3B"}, // negatives render as-is rather than being clamped
	}
	for _, c := range cases {
		assert.That(t, c.in.String()).Equal(c.want)
	}
}

// TestBindByteSize verifies the auto-registered converter binds a ByteSize field
// straight from a property value and a tag default, mirroring time.Duration.
func TestBindByteSize(t *testing.T) {
	t.Run("default value", func(t *testing.T) {
		var s struct {
			MaxMemory conf.ByteSize `value:"${max-memory:=1MB}"`
		}
		p := flatten.NewPropertiesStorage(flatten.NewProperties(nil))
		err := conf.Bind(p, &s)
		assert.That(t, err).Nil()
		assert.That(t, s.MaxMemory).Equal(conf.ByteSize(1 << 20))
	})

	t.Run("from property", func(t *testing.T) {
		var s struct {
			Buf conf.ByteSize `value:"${buf}"`
		}
		p := flatten.NewPropertiesStorage(flatten.MapProperties(map[string]any{
			"buf": "2.5GB",
		}))
		err := conf.Bind(p, &s)
		assert.That(t, err).Nil()
		assert.That(t, s.Buf).Equal(conf.ByteSize(int64(2.5 * (1 << 30))))
	})

	t.Run("invalid value", func(t *testing.T) {
		var s struct {
			Buf conf.ByteSize `value:"${buf:=}"`
		}
		p := flatten.NewPropertiesStorage(flatten.MapProperties(map[string]any{
			"buf": "1QB",
		}))
		err := conf.Bind(p, &s)
		assert.Error(t, err).Matches("unknown suffix")
	})
}
