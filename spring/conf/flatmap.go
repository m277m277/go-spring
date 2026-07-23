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

// FlatMap is a map[string]string that, during configuration binding,
// collects all remaining property key suffixes under a prefix as map keys,
// rather than recursing one level at a time.
//
// Example:
//
//	Properties:
//	  x.a.b=1
//	  x.a.d.e=2
//
//	Binding a FlatMap field tagged with "${x}" produces:
//	  FlatMap{"a.b": "1", "a.d.e": "2"}
//
// FlatMap is useful when you want to capture arbitrary-depth configuration
// keys into a flat string→string mapping. For regular map binding that
// preserves the hierarchical structure, use map[string]T directly.
type FlatMap map[string]string
