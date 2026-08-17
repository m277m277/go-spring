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

package log

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-spring.org/stdlib/testing/assert"
)

func TestDiscardAppender(t *testing.T) {
	a := &DiscardAppender{}
	err := a.Start()
	assert.Error(t, err).Nil()
	a.Append(&Event{})
	a.Stop()
}

func TestConsoleAppender(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "")
		assert.Error(t, err).Nil()

		Stdout = file
		defer func() {
			Stdout = os.Stdout
		}()

		a := &ConsoleAppender{
			AppenderBase: AppenderBase{
				Layout: &TextLayout{
					BaseLayout{
						FileLineMaxLength: 48,
					},
				},
			},
		}
		a.Append(&Event{
			Level:     InfoLevel,
			Time:      time.Time{},
			File:      "file.go",
			Line:      100,
			Tag:       "_def",
			Fields:    []Field{Msg("hello world")},
			CtxFields: nil,
		})

		err = file.Close()
		assert.Error(t, err).Nil()

		b, err := os.ReadFile(file.Name())
		assert.Error(t, err).Nil()
		assert.String(t, string(b)).Equal("[INFO][0001-01-01T00:00:00.000][file.go:100] _def||msg=hello world\n")
	})
}

func TestFileAppender(t *testing.T) {

	t.Run("Start error", func(t *testing.T) {
		a := &FileAppender{
			AppenderBase: AppenderBase{
				Layout: &TextLayout{
					BaseLayout{
						FileLineMaxLength: 48,
					},
				},
			},
			FileName: "/not-exist-dir/file.log",
		}
		err := a.Start()
		assert.Error(t, err).Matches("open /not-exist-dir/file.log: no such file or directory")
	})

	t.Run("success", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "")
		assert.Error(t, err).Nil()
		err = file.Close()
		assert.Error(t, err).Nil()

		a := &FileAppender{
			AppenderBase: AppenderBase{
				Layout: &TextLayout{
					BaseLayout{
						FileLineMaxLength: 48,
					},
				},
			},
			FileName: file.Name(),
		}
		err = a.Start()
		assert.Error(t, err).Nil()

		a.Append(&Event{
			Level:     InfoLevel,
			Time:      time.Time{},
			File:      "file.go",
			Line:      100,
			Tag:       "_def",
			Fields:    []Field{Msg("hello world")},
			CtxFields: nil,
		})

		a.Stop()

		b, err := os.ReadFile(a.file.Name())
		assert.Error(t, err).Nil()
		assert.String(t, string(b)).Equal("[INFO][0001-01-01T00:00:00.000][file.go:100] _def||msg=hello world\n")
	})

	t.Run("stop multiple times", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "")
		assert.Error(t, err).Nil()
		err = file.Close()
		assert.Error(t, err).Nil()

		a := &FileAppender{
			FileName: file.Name(),
		}
		err = a.Start()
		assert.Error(t, err).Nil()

		a.Stop()
		a.Stop()
	})
}

func TestRollingFileAppender(t *testing.T) {

	t.Run("Start error", func(t *testing.T) {
		a := &RollingFileAppender{
			FileName: "/not-exist-dir/file.log",
			Interval: time.Hour,
		}
		err := a.Start()
		assert.Error(t, err).Matches("open /not-exist-dir/file.log.*: no such file or directory")
	})

	t.Run("File name uses truncated interval time", func(t *testing.T) {
		dir := t.TempDir()
		w := &RollingFileWriter{
			fileDir:  dir,
			fileName: "app.log",
			interval: time.Hour,
			maxAge:   time.Hour,
		}
		file, err := w.Rotate()
		assert.Error(t, err).Nil()
		defer w.Close()

		want := "app.log." + time.Now().Truncate(time.Hour).Format("20060102150405")
		assert.That(t, filepath.Base(file.Name())).Equal(want)
	})

	t.Run("Rotate closes the old file after closeDelay and clears expired files", func(t *testing.T) {
		dir := t.TempDir()
		prefix := filepath.Join(dir, "app.log.")
		t.Cleanup(func() {
			closeOpenFilesWithPrefix(prefix)
		})

		// A file older than MaxAge must be removed by the cleanup that
		// runs after the rotated file is closed.
		expiredPath := filepath.Join(dir, "app.log.20200101000000")
		err := os.WriteFile(expiredPath, nil, 0644)
		assert.Error(t, err).Nil()
		expiredTime := time.Now().Add(-2 * time.Hour)
		err = os.Chtimes(expiredPath, expiredTime, expiredTime)
		assert.Error(t, err).Nil()

		w := &RollingFileWriter{
			fileDir:    dir,
			fileName:   "app.log",
			interval:   time.Second,
			maxAge:     time.Hour,
			closeDelay: 10 * time.Millisecond,
		}
		file1, err := w.Rotate()
		assert.Error(t, err).Nil()

		// File names use second resolution, so wait for the next second
		// to force a rotation onto a distinct file.
		now := time.Now()
		time.Sleep(time.Until(now.Truncate(time.Second).Add(1050 * time.Millisecond)))

		file2, err := w.Rotate()
		assert.Error(t, err).Nil()
		assert.That(t, file2.Name() == file1.Name()).False()

		// The rotated file is closed after closeDelay; only the current
		// file stays open. The cleanup removes the expired file but keeps
		// the fresh (not expired) rotated one.
		time.Sleep(100 * time.Millisecond)
		assert.That(t, countOpenFilesWithPrefix(prefix)).Equal(1)

		_, err = os.Stat(file1.Name())
		assert.Error(t, err).Nil()

		_, err = os.Stat(expiredPath)
		assert.That(t, os.IsNotExist(err)).True()

		w.Close()
	})

	t.Run("clearExpiredFiles removes only expired matching files", func(t *testing.T) {
		dir := t.TempDir()

		expiredPath := filepath.Join(dir, "app.log.20200101000000")
		freshPath := filepath.Join(dir, "app.log.20991231235959")
		otherPath := filepath.Join(dir, "other.log.20200101000000")
		expiredDirPath := filepath.Join(dir, "app.log.dir")
		for _, p := range []string{expiredPath, freshPath, otherPath} {
			err := os.WriteFile(p, nil, 0644)
			assert.Error(t, err).Nil()
		}
		err := os.Mkdir(expiredDirPath, 0755)
		assert.Error(t, err).Nil()

		expiredTime := time.Now().Add(-2 * time.Hour)
		for _, p := range []string{expiredPath, otherPath, expiredDirPath} {
			err = os.Chtimes(p, expiredTime, expiredTime)
			assert.Error(t, err).Nil()
		}

		w := &RollingFileWriter{
			fileDir:  dir,
			fileName: "app.log",
			interval: time.Hour,
			maxAge:   time.Hour,
		}
		w.clearExpiredFiles()

		_, err = os.Stat(expiredPath)
		assert.That(t, os.IsNotExist(err)).True()

		// Fresh files, non-matching names and directories are kept.
		for _, p := range []string{freshPath, otherPath, expiredDirPath} {
			_, err = os.Stat(p)
			assert.Error(t, err).Nil()
		}
	})
}
