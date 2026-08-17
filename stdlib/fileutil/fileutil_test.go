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

package fileutil

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

func TestPathExists(t *testing.T) {
	dir := t.TempDir()

	exists, err := PathExists(dir)
	assert.That(t, err).Nil()
	assert.That(t, exists).True("an existing directory reports true")

	file := filepath.Join(dir, "a.txt")
	assert.That(t, os.WriteFile(file, []byte("x"), 0o600)).Nil()

	exists, err = PathExists(file)
	assert.That(t, err).Nil()
	assert.That(t, exists).True("an existing file reports true")

	exists, err = PathExists(filepath.Join(dir, "missing.txt"))
	assert.That(t, err).Nil()
	assert.That(t, exists).False("a missing path reports (false, nil), not an error")
}

// os.Stat follows symlinks, so a dangling link reports the same
// (false, nil) as any other missing path — pin that documented semantics.
func TestPathExists_DanglingSymlink(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions; permission-based cases unreliable")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling")
	assert.That(t, os.Symlink(filepath.Join(dir, "nowhere"), link)).Nil()

	exists, err := PathExists(link)
	assert.That(t, err).Nil()
	assert.That(t, exists).False("a dangling symlink reports false")
}

func TestPathExists_PermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	assert.That(t, os.Chmod(dir, 0o000)).Nil()
	defer func() { _ = os.Chmod(dir, 0o700) }()

	exists, err := PathExists(filepath.Join(dir, "a.txt"))
	assert.That(t, err).NotNil("a stat error other than ErrNotExist is returned")
	assert.That(t, exists).False()
}

func TestReadDirNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		assert.That(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600)).Nil()
	}

	names, err := ReadDirNames(dir)
	assert.That(t, err).Nil()
	sort.Strings(names)
	assert.Slice(t, names).Equal([]string{"a.txt", "b.txt", "c.txt"})
}

func TestReadDirNames_EmptyDir(t *testing.T) {
	names, err := ReadDirNames(t.TempDir())
	assert.That(t, err).Nil()
	assert.Number(t, len(names)).Zero()
}

func TestReadDirNames_MissingDir(t *testing.T) {
	_, err := ReadDirNames(filepath.Join(t.TempDir(), "missing"))
	assert.That(t, err).NotNil()
}

func TestReadDirNames_FileInsteadOfDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	assert.That(t, os.WriteFile(file, nil, 0o600)).Nil()

	_, err := ReadDirNames(file)
	assert.That(t, err).NotNil("opening a regular file for Readdirnames fails")
}
