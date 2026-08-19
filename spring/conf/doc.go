/*
 * Copyright 2024 The Go-Spring Authors.
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

/*
Package conf provides a flexible configuration binding system for Go applications: declarative
binding from various file formats and providers into Go structs with automatic type conversion,
placeholder resolution, and expression-based validation.

# Usage

Struct fields use the value tag - value:"${key:=default}" - to reference configuration keys.
Load a configuration source into a flat property map, then bind it:

	type ServerConfig struct {
		Host string `value:"${server.host:=localhost}"`
		Port int    `value:"${server.port}" expr:"$ > 0 && $ < 65536"`
	}

	p, err := conf.Load("config/")
	if err != nil {
		panic(err)
	}
	var c ServerConfig
	if err := conf.Bind(p, &c); err != nil {
		panic(err)
	}

# Extension

Custom file formats, configuration sources, type converters, decryption schemes, and validation
rules all plug in through Register* functions. See the package README for the full guide (tag
syntax, supported types, value resolution, property-level decryption, validation, loading, and
extension points): https://github.com/go-spring/spring
*/
package conf
