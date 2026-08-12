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

package conf

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ByteSize is a byte count parsed from a human-readable config string such as
// "1KB", "1.5MB", or "1024". It is the byte-domain counterpart of
// [time.Duration]: a scalar int64 with a friendly parser, auto-registered as a
// [Converter] so a struct field of type ByteSize binds straight from properties:
//
//	var cfg struct {
//	    MaxMemory conf.ByteSize `value:"${max-memory:=1MB}"`
//	}
//	// cfg.MaxMemory.Bytes() == 1 << 20
//
// Suffixes are binary (powers of 1024), matching the convention used by JVM
// (-Xmx), nginx, Kafka and most middleware config. Accepted suffixes
// (case-insensitive): B; K, KB, KiB; M, MB, MiB; G, GB, GiB; T, TB, TiB;
// P, PB, PiB. An empty suffix means bytes. A fractional value like "1.5GB" is
// allowed and rounded to the nearest byte. Decimal (power-of-1000) semantics
// are deliberately not supported to avoid the KB-vs-KiB ambiguity in config
// files.
type ByteSize int64

// Bytes returns the size as a plain int64 byte count.
func (b ByteSize) Bytes() int64 { return int64(b) }

// String renders the size in the largest binary unit that keeps the value
// below that unit, e.g. 1536 becomes "1.5KiB". It uses IEC suffixes (KiB, MiB,
// ...) so the binary meaning is unambiguous.
func (b ByteSize) String() string {
	const unit = 1024
	if b < 0 {
		return fmt.Sprintf("%dB", b)
	}
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	units := "KMGTPE"
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < len(units)-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), units[exp])
}

// ParseByteSize parses s into a [ByteSize]. See the [ByteSize] doc comment for
// the accepted grammar. It returns an error describing the offending input and
// the valid suffixes when s is empty, lacks a number, has an unknown suffix,
// is negative, or overflows int64.
func ParseByteSize(s string) (ByteSize, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("conf: empty byte size")
	}
	// Split the leading numeric part (digits and at most dots) from the unit
	// Split the leading numeric part (an optional sign, then digits and dots)
	// from the unit suffix; anything else ends the number.
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' {
			i++
			continue
		}
		break
	}
	numStr := s[:i]
	if numStr == "" || numStr == "+" || numStr == "-" {
		return 0, fmt.Errorf("conf: invalid byte size %q (missing number)", s)
	}
	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("conf: invalid byte size %q: %w", s, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("conf: negative byte size %q", s)
	}
	suffix := strings.ToUpper(strings.TrimSpace(s[i:]))
	factor, ok := sizeFactors[suffix]
	if !ok {
		return 0, fmt.Errorf("conf: invalid byte size %q (unknown suffix %q; want one of B, K/KB, M/MB, G/GB, T/TB, P/PB)", s, suffix)
	}
	val := f * factor
	if val > math.MaxInt64 {
		return 0, fmt.Errorf("conf: byte size %q overflows int64", s)
	}
	return ByteSize(math.Round(val)), nil
}

var sizeFactors = map[string]float64{
	"":    1,
	"B":   1,
	"K":   1024,
	"KB":  1024,
	"KIB": 1024,
	"M":   1024 * 1024,
	"MB":  1024 * 1024,
	"MIB": 1024 * 1024,
	"G":   1024 * 1024 * 1024,
	"GB":  1024 * 1024 * 1024,
	"GIB": 1024 * 1024 * 1024,
	"T":   1024 * 1024 * 1024 * 1024,
	"TB":  1024 * 1024 * 1024 * 1024,
	"TIB": 1024 * 1024 * 1024 * 1024,
	"P":   1024 * 1024 * 1024 * 1024 * 1024,
	"PB":  1024 * 1024 * 1024 * 1024 * 1024,
	"PIB": 1024 * 1024 * 1024 * 1024 * 1024,
}
