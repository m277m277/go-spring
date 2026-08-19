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

package aes

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

// key128 is the base64 of the 16-byte AES key "1234567890123456".
const key128 = "MTIzNDU2Nzg5MDEyMzQ1Ng=="

// newDec builds a decryptor for the current environment; every test sets its
// own key via t.Setenv first.
func newDec(t *testing.T) func(cipherText string) (string, error) {
	t.Helper()
	dec, err := NewDecryptor()
	assert.Error(t, err).Nil()
	return dec
}

func TestRoundTrip(t *testing.T) {
	t.Setenv(EnvKey, key128)
	dec := newDec(t)

	enc, err := Encrypt("s3cr3t-password")
	assert.Error(t, err).Nil()
	got, err := dec(enc)
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal("s3cr3t-password")
}

func TestRoundTripWhitespaceTolerant(t *testing.T) {
	// Ciphertext straight from a YAML/properties editor may carry surrounding
	// whitespace; the decryptor trims it before decoding.
	t.Setenv(EnvKey, key128)
	dec := newDec(t)

	enc, err := Encrypt("v")
	assert.Error(t, err).Nil()
	got, err := dec("  " + enc + "\n")
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal("v")
}

func TestEachEncryptedValueUsesFreshNonce(t *testing.T) {
	// AES-GCM nonce reuse with the same key is catastrophic; two encryptions
	// of the same plaintext must therefore differ.
	t.Setenv(EnvKey, key128)
	a, err := Encrypt("same")
	assert.Error(t, err).Nil()
	b, err := Encrypt("same")
	assert.Error(t, err).Nil()
	assert.That(t, a == b).False()
}

func TestTamperedCiphertextFails(t *testing.T) {
	// Flip a byte in the sealed body: GCM authentication must reject it.
	t.Setenv(EnvKey, key128)
	dec := newDec(t)

	enc, err := Encrypt("secret")
	assert.Error(t, err).Nil()
	raw, err := base64.StdEncoding.DecodeString(enc)
	assert.Error(t, err).Nil()
	raw[len(raw)-1] ^= 0xff
	_, err = dec(base64.StdEncoding.EncodeToString(raw))
	assert.Error(t, err).Matches("AES-GCM open failed")
}

func TestTruncatedCiphertextFails(t *testing.T) {
	t.Setenv(EnvKey, key128)
	dec := newDec(t)

	_, err := dec(base64.StdEncoding.EncodeToString([]byte("short")))
	assert.Error(t, err).Matches("ciphertext too short")
}

func TestBadBase64Fails(t *testing.T) {
	t.Setenv(EnvKey, key128)
	dec := newDec(t)

	_, err := dec("not!base64!")
	assert.Error(t, err).Matches("not valid base64")
}

func TestKeyFromEnv(t *testing.T) {
	t.Setenv(EnvKey, key128)
	t.Setenv(EnvKeyFile, "/nonexistent")
	_, err := NewDecryptor()
	assert.Error(t, err).Nil() // EnvKey wins over EnvKeyFile
}

func TestKeyFromFile(t *testing.T) {
	t.Setenv(EnvKey, "")
	path := filepath.Join(t.TempDir(), "aes.key")
	assert.Error(t, os.WriteFile(path, []byte(key128), 0600)).Nil()
	t.Setenv(EnvKeyFile, path)

	dec := newDec(t)
	enc, err := Encrypt("from-file")
	assert.Error(t, err).Nil()
	got, err := dec(enc)
	assert.Error(t, err).Nil()
	assert.That(t, got).Equal("from-file")
}

func TestKeyFromFileReadFailure(t *testing.T) {
	t.Setenv(EnvKey, "")
	t.Setenv(EnvKeyFile, filepath.Join(t.TempDir(), "missing.key"))
	_, err := NewDecryptor()
	assert.Error(t, err).Matches("read decrypt key file")
}

func TestNoKeyConfigured(t *testing.T) {
	t.Setenv(EnvKey, "")
	t.Setenv(EnvKeyFile, "")
	_, err := NewDecryptor()
	assert.Error(t, err).Matches("no AES decrypt key configured")
}

func TestBadKeyRejected(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"not base64", "!!not-base64!!", "not valid base64"},
		{"too short", base64.StdEncoding.EncodeToString([]byte("short")), "must decode to 16, 24, or 32 bytes"},
		{"not a valid size", base64.StdEncoding.EncodeToString(make([]byte, 20)), "must decode to 16, 24, or 32 bytes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(EnvKey, c.value)
			_, err := NewDecryptor()
			assert.Error(t, err).Matches(c.want)
		})
	}
}

func TestKeySizes(t *testing.T) {
	// AES-128/192/256 are all accepted.
	for _, n := range []int{16, 24, 32} {
		t.Setenv(EnvKey, base64.StdEncoding.EncodeToString(make([]byte, n)))
		dec := newDec(t)
		enc, err := Encrypt("payload")
		assert.Error(t, err).Nil()
		got, err := dec(enc)
		assert.Error(t, err).Nil()
		assert.That(t, got).Equal("payload")
	}
}

func TestWrongKeyFails(t *testing.T) {
	// Encrypt under one key, decrypt under another: GCM must reject.
	t.Setenv(EnvKey, key128)
	enc, err := Encrypt("hello")
	assert.Error(t, err).Nil()

	t.Setenv(EnvKey, base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnop")))
	dec := newDec(t)
	_, err = dec(enc)
	assert.Error(t, err).Matches("AES-GCM open failed")
}
