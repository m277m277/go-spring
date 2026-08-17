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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go-spring.org/stdlib/testing/assert"
)

func TestParseBufferFullPolicy(t *testing.T) {
	_, err := ParseBufferFullPolicy("Block")
	assert.Error(t, err).Matches("invalid BufferFullPolicy Block")

	p, err := ParseBufferFullPolicy("block")
	assert.Error(t, err).Nil()
	assert.That(t, p).Equal(BufferFullPolicyBlock)

	p, err = ParseBufferFullPolicy("discard")
	assert.Error(t, err).Nil()
	assert.That(t, p).Equal(BufferFullPolicyDiscard)

	p, err = ParseBufferFullPolicy("drop-oldest")
	assert.Error(t, err).Nil()
	assert.That(t, p).Equal(BufferFullPolicyDropOldest)
}

// CountAppender wraps an Appender and counts the events it receives.
type CountAppender struct {
	Appender
	// count is read by the test goroutine while the async logger's background
	// goroutine calls Append (which writes it), so it must be atomic — a plain
	// int is a data race under -race even though the 100ms sleep happens to
	// mask it in practice.
	count atomic.Int64
}

func (c *CountAppender) Append(e *Event) {
	c.count.Add(1)
	c.Appender.Append(e)
}

func TestLoggerConfig(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		a := &CountAppender{
			Appender: &DiscardAppender{},
		}

		err := a.Start()
		assert.Error(t, err).Nil()

		l := &SyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
				Tags: []string{"_com_*"},
			},
			AppenderRefs: []*AppenderRef{
				{
					Appender: a,
					Level: LevelRange{
						MinLevel: NoneLevel,
						MaxLevel: MaxLevel,
					},
				},
			},
		}

		err = l.Start()
		assert.Error(t, err).Nil()

		assert.That(t, l.Level.Enable(TraceLevel)).False()
		assert.That(t, l.Level.Enable(DebugLevel)).False()
		assert.That(t, l.Level.Enable(InfoLevel)).True()
		assert.That(t, l.Level.Enable(WarnLevel)).True()
		assert.That(t, l.Level.Enable(ErrorLevel)).True()
		assert.That(t, l.Level.Enable(PanicLevel)).True()
		assert.That(t, l.Level.Enable(FatalLevel)).True()

		for range 5 {
			l.Append(&Event{Level: InfoLevel})
		}

		assert.That(t, a.count.Load()).Equal(int64(5))

		l.Stop()
		a.Stop()
	})

	t.Run("SetLevel override", func(t *testing.T) {
		a := &CountAppender{
			Appender: &DiscardAppender{},
		}

		err := a.Start()
		assert.Error(t, err).Nil()

		l := &SyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
			},
			AppenderRefs: []*AppenderRef{
				{
					Appender: a,
					Level: LevelRange{
						MinLevel: NoneLevel,
						MaxLevel: MaxLevel,
					},
				},
			},
		}

		err = l.Start()
		assert.Error(t, err).Nil()

		l.Append(&Event{Level: InfoLevel})
		assert.That(t, a.count.Load()).Equal(int64(1))

		r := LevelRange{
			MinLevel: WarnLevel,
			MaxLevel: MaxLevel,
		}
		l.SetLevel(r)
		assert.That(t, l.GetLevel()).Equal(r)

		// Info is filtered out on the hot path, Warn still passes.
		l.Append(&Event{Level: InfoLevel})
		l.Append(&Event{Level: WarnLevel})
		assert.That(t, a.count.Load()).Equal(int64(2))

		// The zero LevelRange still installs an override instead of
		// reverting to the configured level.
		l.SetLevel(LevelRange{})
		assert.That(t, l.GetLevel()).Equal(LevelRange{})
		l.Append(&Event{Level: FatalLevel})
		assert.That(t, a.count.Load()).Equal(int64(2))

		l.Stop()
		a.Stop()
	})
}

func TestAsyncLoggerConfig(t *testing.T) {

	t.Run("enable level", func(t *testing.T) {
		l := &AsyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
			},
		}

		assert.That(t, l.Level.Enable(TraceLevel)).False()
		assert.That(t, l.Level.Enable(DebugLevel)).False()
		assert.That(t, l.Level.Enable(InfoLevel)).True()
		assert.That(t, l.Level.Enable(WarnLevel)).True()
		assert.That(t, l.Level.Enable(ErrorLevel)).True()
		assert.That(t, l.Level.Enable(PanicLevel)).True()
		assert.That(t, l.Level.Enable(FatalLevel)).True()
	})

	t.Run("error BufferSize", func(t *testing.T) {
		l := &AsyncLogger{
			LoggerBase: LoggerBase{
				Name: "file",
			},
			BufferSize: 10,
		}

		err := l.Start()
		assert.Error(t, err).Matches("bufferSize 10 is too small, it must be at least 100")
	})

	t.Run("buffer full - discard", func(t *testing.T) {
		a := &CountAppender{
			Appender: &DiscardAppender{},
		}

		err := a.Start()
		assert.Error(t, err).Nil()

		l := &AsyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
				Tags: []string{"_com_*"},
			},
			AppenderRefs: []*AppenderRef{
				{
					Appender: a,
					Level: LevelRange{
						MinLevel: NoneLevel,
						MaxLevel: MaxLevel,
					},
				},
			},
			BufferSize:   100,
			OnBufferFull: BufferFullPolicyDiscard,
		}

		err = l.Start()
		assert.Error(t, err).Nil()

		for range 5000 {
			e := &Event{}
			e.Level = InfoLevel
			l.Append(e)
		}

		time.Sleep(200 * time.Millisecond)

		l.Stop()
		a.Stop()

		// Every event is either delivered to the appender or counted
		// as discarded; none is lost silently.
		assert.That(t, a.count.Load()+l.GetDiscardCounter()).Equal(int64(5000))
		assert.That(t, l.GetDiscardCounter() > 0).True()
	})

	t.Run("buffer full - discard oldest", func(t *testing.T) {
		a := &CountAppender{
			Appender: &DiscardAppender{},
		}

		err := a.Start()
		assert.Error(t, err).Nil()

		l := &AsyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
				Tags: []string{"_com_*"},
			},
			AppenderRefs: []*AppenderRef{
				{
					Appender: a,
					Level: LevelRange{
						MinLevel: NoneLevel,
						MaxLevel: MaxLevel,
					},
				},
			},
			BufferSize:   100,
			OnBufferFull: BufferFullPolicyDropOldest,
		}

		err = l.Start()
		assert.Error(t, err).Nil()

		for range 5000 {
			e := &Event{}
			e.Level = InfoLevel
			l.Append(e)
		}

		time.Sleep(200 * time.Millisecond)

		l.Stop()
		a.Stop()

		// Every event is either delivered to the appender or counted
		// as discarded; none is lost silently.
		assert.That(t, a.count.Load()+l.GetDiscardCounter()).Equal(int64(5000))
		assert.That(t, l.GetDiscardCounter() > 0).True()
	})

	t.Run("buffer full - block", func(t *testing.T) {
		a := &CountAppender{
			Appender: &DiscardAppender{},
		}

		err := a.Start()
		assert.Error(t, err).Nil()

		l := &AsyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
				Tags: []string{"_com_*"},
			},
			AppenderRefs: []*AppenderRef{
				{
					Appender: a,
					Level: LevelRange{
						MinLevel: NoneLevel,
						MaxLevel: MaxLevel,
					},
				},
			},
			BufferSize:   100,
			OnBufferFull: BufferFullPolicyBlock,
		}

		err = l.Start()
		assert.Error(t, err).Nil()

		for range 5000 {
			e := &Event{}
			e.Level = InfoLevel
			l.Append(e)
		}

		l.Stop()
		a.Stop()

		// The block policy never discards: every event is delivered.
		assert.That(t, a.count.Load()).Equal(int64(5000))
		assert.That(t, l.GetDiscardCounter() == 0).True()
	})

	t.Run("success", func(t *testing.T) {
		a := &CountAppender{
			Appender: &DiscardAppender{},
		}

		err := a.Start()
		assert.Error(t, err).Nil()

		l := &AsyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
				Tags: []string{"_com_*"},
			},
			AppenderRefs: []*AppenderRef{
				{
					Appender: a,
					Level: LevelRange{
						MinLevel: NoneLevel,
						MaxLevel: MaxLevel,
					},
				},
			},
			BufferSize: 100,
		}

		err = l.Start()
		assert.Error(t, err).Nil()

		for range 5 {
			e := &Event{}
			e.Level = InfoLevel
			l.Append(e)
		}

		time.Sleep(100 * time.Millisecond)
		assert.That(t, a.count.Load()).Equal(int64(5))

		l.Stop()
		a.Stop()
	})

	t.Run("SetLevel override", func(t *testing.T) {
		a := &CountAppender{
			Appender: &DiscardAppender{},
		}

		err := a.Start()
		assert.Error(t, err).Nil()

		l := &AsyncLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
			},
			AppenderRefs: []*AppenderRef{
				{
					Appender: a,
					Level: LevelRange{
						MinLevel: NoneLevel,
						MaxLevel: MaxLevel,
					},
				},
			},
			BufferSize: 100,
		}

		err = l.Start()
		assert.Error(t, err).Nil()

		l.Append(&Event{Level: InfoLevel})
		time.Sleep(100 * time.Millisecond)
		assert.That(t, a.count.Load()).Equal(int64(1))

		r := LevelRange{
			MinLevel: ErrorLevel,
			MaxLevel: MaxLevel,
		}
		l.SetLevel(r)
		assert.That(t, l.GetLevel()).Equal(r)

		// Info is dropped before enqueueing, Error still passes.
		l.Append(&Event{Level: InfoLevel})
		l.Append(&Event{Level: ErrorLevel})
		time.Sleep(100 * time.Millisecond)
		assert.That(t, a.count.Load()).Equal(int64(2))

		l.Stop()
		a.Stop()
	})
}

func TestDiscardLogger(t *testing.T) {
	l := &DiscardLogger{}
	err := l.Start()
	assert.Error(t, err).Nil()

	assert.String(t, l.GetName()).Equal("")
	assert.That(t, l.GetLevel()).Equal(LevelRange{})

	l.Append(&Event{Level: InfoLevel})
	l.Stop()
}

func TestFileLogger(t *testing.T) {

	t.Run("Start error", func(t *testing.T) {
		l := &FileLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
			},
			Layout:   &TextLayout{BaseLayout{FileLineMaxLength: 48}},
			FileName: "/not-exist-dir/file.log",
		}
		err := l.Start()
		assert.Error(t, err).Matches("open /not-exist-dir/file.log: no such file or directory")
	})

	t.Run("success", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "")
		assert.Error(t, err).Nil()
		err = file.Close()
		assert.Error(t, err).Nil()

		l := &FileLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
			},
			Layout:   &TextLayout{BaseLayout{FileLineMaxLength: 48}},
			FileName: file.Name(),
		}
		err = l.Start()
		assert.Error(t, err).Nil()

		l.Append(&Event{
			Level:  InfoLevel,
			Time:   time.Time{},
			File:   "file.go",
			Line:   100,
			Tag:    "_def",
			Fields: []Field{Msg("hello world")},
		})
		l.Append(&Event{ // below the configured level, dropped
			Level:  DebugLevel,
			Time:   time.Time{},
			File:   "file.go",
			Line:   100,
			Tag:    "_def",
			Fields: []Field{Msg("dropped")},
		})

		l.Stop()

		b, err := os.ReadFile(file.Name())
		assert.Error(t, err).Nil()
		assert.String(t, string(b)).Equal("[INFO][0001-01-01T00:00:00.000][file.go:100] _def||msg=hello world\n")
	})
}

func TestRollingFileLogger(t *testing.T) {

	t.Run("separate mode", func(t *testing.T) {
		dir := t.TempDir()
		prefix := filepath.Join(dir, "app.log")
		t.Cleanup(func() {
			closeOpenFilesWithPrefix(prefix)
		})

		l := &RollingFileLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: DebugLevel,
					MaxLevel: MaxLevel,
				},
			},
			Layout:     &TextLayout{BaseLayout{FileLineMaxLength: 48}},
			FileDir:    dir,
			FileName:   "app.log",
			Interval:   time.Hour,
			Separate:   true,
			AsyncWrite: false,
		}
		err := l.Start()
		assert.Error(t, err).Nil()

		for _, lvl := range []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel} {
			l.Append(&Event{
				Level:  lvl,
				Time:   time.Time{},
				File:   "file.go",
				Line:   100,
				Tag:    "_def",
				Fields: []Field{Msg("hello " + lvl.LowerName())},
			})
		}

		l.Stop()

		// The normal file holds levels below WARN, the ".wf" file WARN and above.
		normalFiles, err := filepath.Glob(filepath.Join(dir, "app.log.[0-9]*"))
		assert.Error(t, err).Nil()
		assert.That(t, len(normalFiles)).Equal(1)

		wfFiles, err := filepath.Glob(filepath.Join(dir, "app.log.wf.[0-9]*"))
		assert.Error(t, err).Nil()
		assert.That(t, len(wfFiles)).Equal(1)

		b, err := os.ReadFile(normalFiles[0])
		assert.Error(t, err).Nil()
		assert.String(t, string(b)).Equal(
			"[DEBUG][0001-01-01T00:00:00.000][file.go:100] _def||msg=hello debug\n" +
				"[INFO][0001-01-01T00:00:00.000][file.go:100] _def||msg=hello info\n")

		b, err = os.ReadFile(wfFiles[0])
		assert.Error(t, err).Nil()
		assert.String(t, string(b)).Equal(
			"[WARN][0001-01-01T00:00:00.000][file.go:100] _def||msg=hello warn\n" +
				"[ERROR][0001-01-01T00:00:00.000][file.go:100] _def||msg=hello error\n")
	})

	t.Run("SetLevel propagates to the internal logger", func(t *testing.T) {
		dir := t.TempDir()
		prefix := filepath.Join(dir, "app.log")
		t.Cleanup(func() {
			closeOpenFilesWithPrefix(prefix)
		})

		l := &RollingFileLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: DebugLevel,
					MaxLevel: MaxLevel,
				},
			},
			Layout:   &TextLayout{BaseLayout{FileLineMaxLength: 48}},
			FileDir:  dir,
			FileName: "app.log",
			Interval: time.Hour,
		}
		err := l.Start()
		assert.Error(t, err).Nil()

		l.Append(&Event{
			Level:  DebugLevel,
			Time:   time.Time{},
			File:   "file.go",
			Line:   100,
			Tag:    "_def",
			Fields: []Field{Msg("before")},
		})

		r := LevelRange{
			MinLevel: ErrorLevel,
			MaxLevel: MaxLevel,
		}
		l.SetLevel(r)
		assert.That(t, l.GetLevel()).Equal(r)
		assert.That(t, l.logger.GetLevel()).Equal(r)

		// Info is filtered out on the hot path, Error still passes.
		l.Append(&Event{
			Level:  InfoLevel,
			Time:   time.Time{},
			File:   "file.go",
			Line:   100,
			Tag:    "_def",
			Fields: []Field{Msg("dropped")},
		})
		l.Append(&Event{
			Level:  ErrorLevel,
			Time:   time.Time{},
			File:   "file.go",
			Line:   100,
			Tag:    "_def",
			Fields: []Field{Msg("after")},
		})

		l.Stop()

		files, err := filepath.Glob(filepath.Join(dir, "app.log.[0-9]*"))
		assert.Error(t, err).Nil()
		assert.That(t, len(files)).Equal(1)

		b, err := os.ReadFile(files[0])
		assert.Error(t, err).Nil()
		assert.String(t, string(b)).Equal(
			"[DEBUG][0001-01-01T00:00:00.000][file.go:100] _def||msg=before\n" +
				"[ERROR][0001-01-01T00:00:00.000][file.go:100] _def||msg=after\n")
	})

	t.Run("SetLevel before Start", func(t *testing.T) {
		l := &RollingFileLogger{
			LoggerBase: LoggerBase{
				Level: LevelRange{
					MinLevel: InfoLevel,
					MaxLevel: MaxLevel,
				},
			},
			Layout:   &TextLayout{BaseLayout{FileLineMaxLength: 48}},
			FileDir:  t.TempDir(),
			FileName: "app.log",
			Interval: time.Hour,
		}

		// The internal logger doesn't exist yet; SetLevel must not panic.
		r := LevelRange{
			MinLevel: ErrorLevel,
			MaxLevel: MaxLevel,
		}
		l.SetLevel(r)
		assert.That(t, l.GetLevel()).Equal(r)
	})
}

func TestRollingFileLoggerStartErrorCleansAppenders(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "app.log.")
	t.Cleanup(func() {
		closeOpenFilesWithPrefix(prefix)
	})

	l := &RollingFileLogger{
		LoggerBase: LoggerBase{
			Level: LevelRange{
				MinLevel: InfoLevel,
				MaxLevel: MaxLevel,
			},
		},
		Layout:       &TextLayout{},
		FileDir:      dir,
		FileName:     "app.log",
		Interval:     time.Hour,
		AsyncWrite:   true,
		BufferSize:   10,
		OnBufferFull: BufferFullPolicyDiscard,
	}

	err := l.Start()
	assert.Error(t, err).Matches("bufferSize 10 is too small, it must be at least 100")
	assert.That(t, countOpenFilesWithPrefix(prefix)).Equal(0)
}

func countOpenFilesWithPrefix(prefix string) int {
	fileManager.mutex.Lock()
	defer fileManager.mutex.Unlock()

	count := 0
	for name := range fileManager.files {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	return count
}

func closeOpenFilesWithPrefix(prefix string) {
	for {
		fileManager.mutex.Lock()
		var f *File
		for name, openFile := range fileManager.files {
			if strings.HasPrefix(name, prefix) {
				f = openFile
				break
			}
		}
		fileManager.mutex.Unlock()

		if f == nil {
			return
		}
		CloseFile(f)
	}
}
