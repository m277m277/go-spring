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

package mathutil

import (
	"math"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

// Defined types with predeclared underlying types: the overflow checks must
// respect the underlying bounds, not fall through to "no overflow".
type myInt8 int8
type myInt16 int16
type myUint8 uint8
type myFloat32 float32

func TestOverflowInt_Bounds(t *testing.T) {
	assert.That(t, OverflowInt[int8](math.MaxInt8)).False()
	assert.That(t, OverflowInt[int8](math.MaxInt8+1)).True()
	assert.That(t, OverflowInt[int8](math.MinInt8)).False()
	assert.That(t, OverflowInt[int8](math.MinInt8-1)).True()

	assert.That(t, OverflowInt[int16](math.MaxInt16)).False()
	assert.That(t, OverflowInt[int16](math.MaxInt16+1)).True()
	assert.That(t, OverflowInt[int16](math.MinInt16)).False()
	assert.That(t, OverflowInt[int16](math.MinInt16-1)).True()

	assert.That(t, OverflowInt[int32](math.MaxInt32)).False()
	assert.That(t, OverflowInt[int32](math.MaxInt32+1)).True()
	assert.That(t, OverflowInt[int32](math.MinInt32)).False()
	assert.That(t, OverflowInt[int32](math.MinInt32-1)).True()

	// int and int64 share the int64 input domain, so nothing overflows.
	assert.That(t, OverflowInt[int](math.MaxInt64)).False()
	assert.That(t, OverflowInt[int64](math.MinInt64)).False()
}

func TestOverflowInt_DefinedTypes(t *testing.T) {
	assert.That(t, OverflowInt[myInt8](200)).True("type myInt8 int8 overflows at 127")
	assert.That(t, OverflowInt[myInt16](40000)).True("type myInt16 int16 overflows at 32767")
	assert.That(t, OverflowInt[myInt8](100)).False()
}

func TestOverflowUint_Bounds(t *testing.T) {
	assert.That(t, OverflowUint[uint8](math.MaxUint8)).False()
	assert.That(t, OverflowUint[uint8](math.MaxUint8+1)).True()

	assert.That(t, OverflowUint[uint16](math.MaxUint16)).False()
	assert.That(t, OverflowUint[uint16](math.MaxUint16+1)).True()

	assert.That(t, OverflowUint[uint32](math.MaxUint32)).False()
	assert.That(t, OverflowUint[uint32](math.MaxUint32+1)).True()

	assert.That(t, OverflowUint[uint](math.MaxUint64)).False()
	assert.That(t, OverflowUint[uint64](math.MaxUint64)).False()
}

func TestOverflowUint_DefinedTypes(t *testing.T) {
	assert.That(t, OverflowUint[myUint8](300)).True("type myUint8 uint8 overflows at 255")
	assert.That(t, OverflowUint[myUint8](255)).False()
}

func TestOverflowFloat_Bounds(t *testing.T) {
	assert.That(t, OverflowFloat[float32](math.MaxFloat32)).False()
	assert.That(t, OverflowFloat[float32](math.Nextafter(math.MaxFloat32, math.MaxFloat64))).True()
	assert.That(t, OverflowFloat[float32](-math.MaxFloat32)).False()
	assert.That(t, OverflowFloat[float32](-math.Nextafter(math.MaxFloat32, math.MaxFloat64))).True()
	assert.That(t, OverflowFloat[float32](1e-40)).False("subnormals fit float32")
	assert.That(t, OverflowFloat[float32](math.NaN())).False("NaN never reports overflow")

	assert.That(t, OverflowFloat[float64](math.MaxFloat64)).False()
	assert.That(t, OverflowFloat[float64](math.SmallestNonzeroFloat64)).False()
}

func TestOverflowFloat_DefinedTypes(t *testing.T) {
	assert.That(t, OverflowFloat[myFloat32](1e39)).True("type myFloat32 float32 overflows near 3.4e38")
	assert.That(t, OverflowFloat[myFloat32](1.0)).False()
}
