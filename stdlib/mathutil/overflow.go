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

// Package mathutil provides overflow checking for integer, unsigned, and
// float conversions ([OverflowInt], [OverflowUint], [OverflowFloat]). The
// target type is a type parameter, so a defined type (e.g. `type ID int32`)
// is checked against its underlying size.
package mathutil

import (
	"math"
	"unsafe"
)

// OverflowInt reports whether the int64 value v exceeds the bounds of the
// target integer type T. T may be a predeclared integer type or a defined
// type with such an underlying type (e.g. `type ID int32`).
func OverflowInt[T ~int | ~int8 | ~int16 | ~int32 | ~int64](v int64) bool {
	var z T
	switch unsafe.Sizeof(z) {
	case 1:
		return v > math.MaxInt8 || v < math.MinInt8
	case 2:
		return v > math.MaxInt16 || v < math.MinInt16
	case 4:
		return v > math.MaxInt32 || v < math.MinInt32
	default: // 8 bytes: int on 64-bit platforms and int64
		return false
	}
}

// OverflowUint reports whether the uint64 value v exceeds the bounds of the
// target unsigned integer type T. T may be a predeclared unsigned type or a
// defined type with such an underlying type.
func OverflowUint[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64](v uint64) bool {
	var z T
	switch unsafe.Sizeof(z) {
	case 1:
		return v > math.MaxUint8
	case 2:
		return v > math.MaxUint16
	case 4:
		return v > math.MaxUint32
	default: // 8 bytes: uint on 64-bit platforms and uint64
		return false
	}
}

// OverflowFloat reports whether the float64 value v exceeds the bounds of the
// target float type T. T may be float32, float64, or a defined type with such
// an underlying type. NaN never reports overflow (it compares false against
// every bound).
func OverflowFloat[T ~float32 | ~float64](v float64) bool {
	var z T
	switch unsafe.Sizeof(z) {
	case 4:
		return v > math.MaxFloat32 || v < -math.MaxFloat32
	default: // 8 bytes: float64
		return false
	}
}
