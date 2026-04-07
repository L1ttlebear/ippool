package log

import (
	"context"
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"os"
	"runtime"
	"time"
)

func Green(format string, v ...interface{}) string {
	return fmt.Sprintf("\033[32m"+format+"\033[0m", v...)
}

func Yellow(format string, v ...interface{}) string {
	return fmt.Sprintf("\033[33m"+format+"\033[0m", v...)
}

func Red(format string, v ...interface{}) string {
	return fmt.Sprintf("\033[31m"+format+"\033[0m", v...)
}

func Blue(format string, v ...interface{}) string {
	return fmt.Sprintf("\033[34m"+format+"\033[0m", v...)
}

func Cyan(format string, v ...interface{}) string {
	return fmt.Sprintf("\033[36m"+format+"\033[0m", v...)
}

func Gray(format string, v ...interface{}) string {
	return fmt.Sprintf("\033[90m"+format+"\033[0m", v...)
}

func White(format string, v ...interface{}) string {
	return fmt.Sprintf("\033[37m"+format+"\033[0m", v...)
}

type LogHandler struct {
	w     io.Writer
	level slog.Level
	group string
}

func NewHandler(w io.Writer, level slog.Level) *LogHandler {
	return &LogHandler{w: w, level: level, group: ""}
}

func (h *LogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *LogHandler) Handle(_ context.Context, r slog.Record) error {
	var file string
	var line int
	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		file = f.File
		line = f.Line
	}

	timeStr := r.Time.Format("2006/01/02 15:04:05")

	group := h.group
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "_group" {
			group = a.Value.String()
			return false
		}
		return true
	})

	levelStr := ""
	switch r.Level {
	case slog.LevelDebug:
		if group != "" {
			levelStr = Cyan("[DEBUG/%s]", group)
		} else {
			levelStr = Cyan("[DEBUG]")
		}
	case slog.LevelInfo:
		if group != "" {
			levelStr = Green("[INFO/%s]", group)
		} else {
			levelStr = Green("[INFO]")
		}
	case slog.LevelWarn:
		if group != "" {
			levelStr = Yellow("[WARN/%s]", group)
		} else {
			levelStr = Yellow("[WARN]")
		}
	case slog.LevelError:
		if group != "" {
			levelStr = Red("[ERROR/%s]", group)
		} else {
			levelStr = Red("[ERROR]")
		}
	}

	var msg string
	if file != "" {
		msg = fmt.Sprintf("%s %s %s %s", timeStr, levelStr, r.Message,
			Gray("(%s:%d)", file, line))
	} else {
		msg = fmt.Sprintf("%s %s %s", timeStr, levelStr, r.Message)
	}

	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "_group" {
			msg += fmt.Sprintf(" %s=%s", Cyan(a.Key), Yellow("%v", a.Value))
		}
		return true
	})

	msg += "\n"
	_, err := h.w.Write([]byte(msg))
	return err
}

func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *LogHandler) WithGroup(name string) slog.Handler {
	return h
}

func SetupGlobalLogger(level slog.Level) {
	handler := NewHandler(os.Stdout, level)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	stdlog.SetOutput(os.Stdout)
	stdlog.SetFlags(0)
	stdlog.SetPrefix("")
	stdlog.SetOutput(&writerAdapter{handler: handler, level: level})
}

type writerAdapter struct {
	handler slog.Handler
	level   slog.Level
}

func (w *writerAdapter) Write(p []byte) (n int, err error) {
	msg := string(p)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	var pcs [1]uintptr
	runtime.Callers(4, pcs[:])
	r := slog.NewRecord(time.Now(), w.level, msg, pcs[0])
	return len(p), w.handler.Handle(context.Background(), r)
}

func GetWriter() io.Writer {
	handler := slog.Default().Handler()
	return &writerAdapter{handler: handler, level: slog.LevelInfo}
}
