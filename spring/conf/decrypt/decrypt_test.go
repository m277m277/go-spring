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

package decrypt_test

import (
	"encoding/base64"
	"os"
	"strings"
	"sync"
	"testing"

	"go-spring.org/spring/conf/decrypt"
	"go-spring.org/spring/conf/decrypt/aes"
	"go-spring.org/stdlib/testing/assert"
)

// key128 is a 16-byte AES key, base64-encoded, used across the tests.
const key128 = "MTIzNDU2Nzg5MDEyMzQ1Ng==" // "1234567890123456"

func TestMarkerShapes(t *testing.T) {
	// Marker recognition from the outside: values that carry a usable marker
	// fail (no key configured), everything else passes through unchanged.
	t.Setenv(aes.EnvKey, "")
	t.Setenv(aes.EnvKeyFile, "")

	for _, v := range []string{"ENC(abc)", "{cipher}abc", "ENC(nope:abc)", "ENC()", "{cipher}"} {
		_, err := decrypt.Decode(v)
		assert.Error(t, err).NotNil(v)
	}
	for _, v := range []string{"plain", "ENC(", "prefixENC(abc)"} {
		got, err := decrypt.Decode(v)
		assert.Error(t, err).Nil(v)
		assert.That(t, got).Equal(v, v)
	}
}

// newDriver registers the AES constructor under a name unique to the test, so
// the per-name decryptor cache starts empty for it. This keeps tests isolated
// without reaching into package internals to reset the shared cache.
func newDriver(t *testing.T) string {
	t.Helper()
	name := "aes-" + strings.ReplaceAll(t.Name(), "/", "-")
	decrypt.Register(name, aes.NewDecryptor)
	return name
}

func TestAESRoundTrip(t *testing.T) {
	t.Setenv(aes.EnvKey, key128)
	drv := newDriver(t)

	plain := "s3cr3t-password"
	enc, err := aes.Encrypt(plain)
	assert.Error(t, err).Nil()

	got, err := decrypt.Decode("ENC(" + drv + ":" + enc + ")")
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal(plain)

	got, err = decrypt.Decode("{cipher}" + drv + ":" + enc)
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal(plain)
}

func TestDecodePlainPassthrough(t *testing.T) {
	// No marker: value returns unchanged and no decryptor is built, so an
	// absent key must not matter.
	t.Setenv(aes.EnvKey, "")
	got, err := decrypt.Decode("localhost:5432")
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal("localhost:5432")
}

func TestDecodeMissingKeyFailsFast(t *testing.T) {
	t.Setenv(aes.EnvKey, "")
	t.Setenv(aes.EnvKeyFile, "")
	drv := newDriver(t)
	_, err := decrypt.Decode("ENC(" + drv + ":whatever)")
	assert.Error(t, err).Matches("no AES decrypt key configured")
}

func TestUnnamedMarkerFailsFast(t *testing.T) {
	// A marker whose prefix is not a registered driver name leaves the marker
	// unnamed; without a default driver that is a fail-fast error.
	_, err := decrypt.Decode("ENC(whatever)")
	assert.Error(t, err).Matches("marker must name a driver")
}

func TestDecodeWrongKeyFailsFast(t *testing.T) {
	// Encrypt under one key ...
	t.Setenv(aes.EnvKey, key128)
	enc, err := aes.Encrypt("hello")
	assert.Error(t, err).Nil()

	// ... then decrypt under a different key via a freshly built driver.
	otherKey := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnop"))
	t.Setenv(aes.EnvKey, otherKey)
	drv := newDriver(t)
	_, err = decrypt.Decode("ENC(" + drv + ":" + enc + ")")
	assert.Error(t, err).Matches("AES-GCM open failed")
}

func TestDecodeBadBase64(t *testing.T) {
	t.Setenv(aes.EnvKey, key128)
	drv := newDriver(t)
	_, err := decrypt.Decode("ENC(" + drv + ":not!base64!)")
	assert.Error(t, err).Matches("not valid base64")
}

func TestLoadKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/aes.key"
	err := os.WriteFile(path, []byte(key128), 0600)
	assert.Error(t, err).Nil()

	t.Setenv(aes.EnvKey, "")
	t.Setenv(aes.EnvKeyFile, path)
	drv := newDriver(t)

	enc, err := aes.Encrypt("from-file")
	assert.Error(t, err).Nil()
	got, err := decrypt.Decode("ENC(" + drv + ":" + enc + ")")
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal("from-file")
}

func TestBadKeyLength(t *testing.T) {
	t.Setenv(aes.EnvKey, base64.StdEncoding.EncodeToString([]byte("too-short")))
	drv := newDriver(t)
	_, err := decrypt.Decode("ENC(" + drv + ":anything)")
	assert.Error(t, err).Matches("decrypt key must decode to 16, 24, or 32 bytes")
}

func TestRegisterPanics(t *testing.T) {
	assert.Panic(t, func() { decrypt.Register("", func() (decrypt.Decryptor, error) { return nil, nil }) }, "cannot be empty")
	assert.Panic(t, func() { decrypt.Register("x", nil) }, "cannot be nil")
	assert.Panic(t, func() { decrypt.Register("aes", func() (decrypt.Decryptor, error) { return nil, nil }) }, "already exists")
}

func TestColonInsideCiphertext(t *testing.T) {
	// Only the first colon splits driver from ciphertext; aes ciphertext is
	// base64 (colon-free) but the split must not be confused by later colons.
	t.Setenv(aes.EnvKey, key128)
	drv := newDriver(t)
	_, err := decrypt.Decode("ENC(" + drv + ":nope:x)")
	assert.Error(t, err).Matches("not valid base64")
}

// fake is a second registered driver so tests can exercise markers that
// dispatch between drivers by name.
func init() {
	decrypt.Register("fake", func() (decrypt.Decryptor, error) {
		return func(cipherText string) (string, error) { return "fake:" + cipherText, nil }, nil
	})
}

func TestMarkerNamesDriver(t *testing.T) {
	t.Setenv(aes.EnvKey, key128)
	drv := newDriver(t)

	enc, err := aes.Encrypt("secret")
	assert.Error(t, err).Nil()
	got, err := decrypt.Decode("ENC(" + drv + ":" + enc + ")")
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal("secret")

	// A different registered driver is dispatched by name, so several schemes
	// can coexist in one file.
	got, err = decrypt.Decode("{cipher}fake:payload")
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal("fake:payload")

	// A prefix that is not a registered driver leaves the marker unnamed,
	// which fails fast instead of guessing.
	_, err = decrypt.Decode("ENC(nosuch:payload)")
	assert.Error(t, err).Matches("marker must name a driver")
}

func TestBuildErrorIsCached(t *testing.T) {
	// A missing key fails the build; the error is cached, so the second marked
	// property fails with the same error without re-reading the environment.
	t.Setenv(aes.EnvKey, "")
	t.Setenv(aes.EnvKeyFile, "")
	drv := newDriver(t)

	for i := 0; i < 2; i++ {
		_, err := decrypt.Decode("ENC(" + drv + ":whatever)")
		assert.Error(t, err).Matches("no AES decrypt key configured")
	}
}

func TestDecodeConcurrent(t *testing.T) {
	// Many goroutines hitting an unbuilt driver at once must build it exactly
	// once and all see the same plaintext (run under -race).
	t.Setenv(aes.EnvKey, key128)
	drv := newDriver(t)

	enc, err := aes.Encrypt("concurrent")
	assert.Error(t, err).Nil()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := decrypt.Decode("ENC(" + drv + ":" + enc + ")")
			assert.Error(t, err).Nil()
			assert.That(t, got).Equal("concurrent")
		}()
	}
	wg.Wait()
}
