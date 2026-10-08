// SPDX-License-Identifier: AGPL-3.0-or-later
package cargohold

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"
)

var logLevels = []string{"Trace", "Debug", "Info", "Warn", "Error", "Fatal"}
var logFormats = []string{"Plain", "Netstring", "JSON", "StructuredJSON"}

func member(value string, choices []string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

type logField struct{ key, value string }
type Logger struct {
	mu                sync.Mutex
	output            io.Writer
	threshold, format string
}

func NewLogger(config Config, output io.Writer) *Logger {
	format := "Plain"
	if config.LogNetStrings {
		format = "Netstring"
	}
	if config.LogFormat != nil {
		format = *config.LogFormat
	}
	return &Logger{output: output, threshold: config.LogLevel, format: format}
}

func canonical(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, value)
}

// Event preserves the configured Wire log shapes and threshold. Field values
// remain data; whitespace is canonicalized to prevent injected log lines.
func (l *Logger) Event(level, message string, fields ...logField) {
	if l == nil {
		return
	}
	rank := func(level string) int {
		for i, candidate := range logLevels {
			if candidate == level {
				return i
			}
		}
		return 2
	}
	if rank(level) < rank(l.threshold) {
		return
	}
	message = canonical(message)
	for i := range fields {
		fields[i].value = canonical(fields[i].value)
	}
	var line string
	switch l.format {
	case "JSON", "StructuredJSON":
		messages := []string{message}
		object := map[string]any{}
		if l.format == "JSON" {
			messages = append([]string{level[:1]}, messages...)
		} else {
			object["level"] = level
		}
		object["msgs"] = messages
		for _, f := range fields {
			object[f.key] = f.value
		}
		b, _ := json.Marshal(object)
		line = string(b)
	case "Netstring":
		// Tinylog encodes each element separately, including the '=' delimiter:
		// https://gitlab.com/twittner/tinylog/-/raw/master/src/System/Logger/Message.hs
		netstr := func(value string) string { return fmt.Sprintf("%d:%s,", len(value), value) }
		line = netstr(level[:1])
		for _, f := range fields {
			line += netstr(f.key) + "1:=," + netstr(f.value)
		}
		line += netstr(message)
	default:
		parts := []string{level[:1]}
		for _, f := range fields {
			parts = append(parts, f.key+"="+f.value)
		}
		parts = append(parts, message)
		line = strings.Join(parts, ", ")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.output, line+"\n")
}

func (l *Logger) Listening(address string) { l.Event("Info", "Listening on "+address) }
