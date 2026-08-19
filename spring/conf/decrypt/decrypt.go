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

// Package decrypt provides property-level decryption for the conf binding
// pipeline, the Go-Spring equivalent of Jasypt's ENC(...) and Spring Cloud
// Config's {cipher}... markers.
//
// A property whose resolved value is wrapped in one of the recognized markers
//
//	password=ENC(<ciphertext>)
//	password={cipher}<ciphertext>
//
// is decrypted right before it is bound, so application code and downstream
// starters only ever see the plaintext. The marker may also name the driver
// explicitly - password=ENC(aes:<ciphertext>) - so several decryption schemes
// can coexist in one configuration file. Plain (unwrapped) values pass through
// untouched, so enabling the feature has no effect on configuration that does
// not use a marker.
//
// The decryptor is pluggable through a driver registry that mirrors the
// conf provider/converter registries (panic on empty/nil/duplicate). A
// symmetric AES-GCM driver ("aes") ships built in; a company can register its
// own driver — an asymmetric scheme or a cloud KMS client — and select it with
// naming it in the marker without forking conf.
//
// The decryption key itself never lives in a configuration file: the built-in
// driver reads it from an environment variable or a mounted file (see the aes
// subpackage).
// A value that carries a marker but cannot be decrypted fails startup with a
// clear errutil error rather than degrading to a half-working default.
package decrypt

import (
	"strings"
	"sync"

	"go-spring.org/spring/conf/decrypt/aes"
	"go-spring.org/stdlib/errutil"
)

// Decryptor turns a ciphertext (the inner text of an ENC(...) / {cipher}...
// marker) into its plaintext form. It is a plain function signature (a type
// alias), so a driver package does not need to import decrypt to implement it -
// the same trick conf/reader uses to register its json/yaml/toml/prop
// subpackages without them importing reader.
type Decryptor = func(cipherText string) (plainText string, err error)

// Factory builds a Decryptor. It is invoked lazily, the first time a marked
// property is encountered, and reads its key material from the environment or a
// mounted file at that point. Returning an error makes the marked property fail
// to bind (fail-fast).
type Factory = func() (Decryptor, error)

var decryptors = map[string]Factory{}

func init() {
	Register("aes", aes.NewDecryptor)
}

// Register registers a decryptor Factory under name. It follows the same
// panic-on-empty/nil/duplicate convention as the other conf registries and must
// be called from an init function only.
func Register(name string, f Factory) {
	if name == "" {
		panic("decrypt driver name cannot be empty")
	}
	if f == nil {
		panic("decrypt driver " + name + " cannot be nil")
	}
	if _, ok := decryptors[name]; ok {
		panic("decrypt driver " + name + " already exists")
	}
	decryptors[name] = f
}

// Markers recognized on a resolved property value. Both the Jasypt-style
// ENC(...) wrapper and the Spring Cloud Config {cipher} prefix are accepted.
const (
	encPrefix    = "ENC("
	encSuffix    = ")"
	cipherPrefix = "{cipher}"
)

// unwrap reports whether value carries a decryption marker and, if so, returns
// the named driver and the ciphertext. The inner text must start with "name:"
// where name is a registered driver, so the decryption scheme is always
// explicit and one configuration file can mix schemes (a migration from AES to
// a cloud KMS, several key systems side by side). A marker whose prefix is not
// a registered driver name leaves the driver empty. A value without a marker
// returns ("", "", false).
//
// Reading the decryptors map here is race-free because Register only runs from
// init functions, before any property is bound.
func unwrap(value string) (driver, cipherText string, marked bool) {
	var inner string
	switch {
	case strings.HasPrefix(value, cipherPrefix):
		inner = value[len(cipherPrefix):]
	case strings.HasPrefix(value, encPrefix) && strings.HasSuffix(value, encSuffix):
		inner = value[len(encPrefix) : len(value)-len(encSuffix)]
	default:
		return "", "", false
	}
	if name, rest, ok := strings.Cut(inner, ":"); ok {
		if _, registered := decryptors[name]; registered {
			return name, rest, true
		}
	}
	return "", inner, true
}

// cacheEntry holds a resolved decryptor and any build error, so each driver's
// key is read and its instance constructed only once per process.
type cacheEntry struct {
	decryptor Decryptor
	err       error
}

// cache holds resolved decryptors by driver name. Errors are cached too: a
// driver whose key is missing or malformed fails every marked property, not
// just the first one that triggered the build.
var cache = struct {
	sync.Mutex
	entries map[string]cacheEntry
}{entries: map[string]cacheEntry{}}

// decryptorFor resolves and caches the decryptor for name. An unknown driver
// is a fail-fast error; there is no default.
func decryptorFor(name string) (Decryptor, error) {
	cache.Lock()
	defer cache.Unlock()
	if e, ok := cache.entries[name]; ok {
		return e.decryptor, e.err
	}
	f, ok := decryptors[name]
	if !ok {
		e := cacheEntry{err: errutil.Explain(nil, "unknown decrypt driver %q", name)}
		cache.entries[name] = e
		return e.decryptor, e.err
	}
	dec, err := f()
	cache.entries[name] = cacheEntry{decryptor: dec, err: err}
	return dec, err
}

// Decode returns the plaintext for a resolved property value. Values without a
// decryption marker are returned unchanged. A marked value must name its
// driver - ENC(name:cipher) - and is decrypted with that driver; a marker
// without a usable driver name or a decrypt failure is surfaced so the caller
// can fail startup.
func Decode(value string) (string, error) {
	name, cipherText, marked := unwrap(value)
	if !marked {
		return value, nil
	}
	if name == "" {
		return "", errutil.Explain(nil, "decrypt marker must name a driver, e.g. ENC(aes:<ciphertext>)")
	}
	dec, err := decryptorFor(name)
	if err != nil {
		return "", errutil.Explain(err, "decrypt property value failed")
	}
	plain, err := dec(cipherText)
	if err != nil {
		return "", errutil.Explain(err, "decrypt property value failed")
	}
	return plain, nil
}
