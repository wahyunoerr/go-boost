package logs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxTailBytes = 4 << 20
	maxLineBytes = 1 << 20
)

type LogEntry struct {
	Timestamp string `json:"timestamp,omitempty"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Caller    string `json:"caller,omitempty"`
	Stack     string `json:"stack,omitempty"`
	Raw       string `json:"raw"`
	Source    string `json:"source"`
}

var (
	textTimestampRegex = regexp.MustCompile(`^\[?\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	logfmtRegex        = regexp.MustCompile(`(?:^|\s)(time|ts|level|lvl|msg|message)=`)
	levelRegex         = regexp.MustCompile(`(?i)\b(PANIC|FATAL|ERROR|ERR|WARN(?:ING)?|INFO|DEBUG|TRACE)\b`)
)

func FindLogFiles(rootDir string) []string {
	var files []string

	candidates := []string{
		"app.log", "server.log", "debug.log", "error.log", "application.log",
		"logs/app.log", "logs/error.log", "logs/server.log", "logs/debug.log",
		"storage/logs/app.log", "var/log/app.log", "tmp/app.log",
	}

	for _, c := range candidates {
		p := filepath.Join(rootDir, c)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			files = append(files, p)
		}
	}

	for _, dirName := range []string{"logs", "log", filepath.Join("storage", "logs")} {
		logsDir := filepath.Join(rootDir, dirName)
		fi, err := os.Stat(logsDir)
		if err != nil || !fi.IsDir() {
			continue
		}
		_ = filepath.Walk(logsDir, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi == nil || fi.IsDir() {
				return nil
			}
			name := strings.ToLower(fi.Name())
			if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".ndjson") {
				files = append(files, path)
			}
			return nil
		})
	}

	files = uniqueStrings(files)

	sort.SliceStable(files, func(i, j int) bool {
		fi, err1 := os.Stat(files[i])
		fj, err2 := os.Stat(files[j])
		if err1 != nil || err2 != nil {
			return files[i] < files[j]
		}
		return fi.ModTime().Before(fj.ModTime())
	})

	return files
}

func ReadEntries(rootDir string, maxEntries int) ([]LogEntry, error) {
	if maxEntries <= 0 {
		maxEntries = 50
	}

	logFiles := FindLogFiles(rootDir)
	if len(logFiles) == 0 {
		return []LogEntry{}, nil
	}

	var allEntries []LogEntry
	for _, file := range logFiles {
		entries, err := parseLogFile(file, maxEntries)
		if err == nil {
			allEntries = append(allEntries, entries...)
		}
	}

	if len(allEntries) > maxEntries {
		return allEntries[len(allEntries)-maxEntries:], nil
	}
	return allEntries, nil
}

func readTail(path string, limit int64) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	offset := int64(0)
	if info.Size() > limit {
		offset = info.Size() - limit
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	if offset > 0 {
		_, _ = reader.ReadString('\n')
	}

	var lines []string
	var current strings.Builder
	for {
		chunk, err := reader.ReadString('\n')
		if len(chunk) > 0 {
			trimmed := strings.TrimRight(chunk, "\r\n")
			if current.Len() < maxLineBytes {
				current.WriteString(trimmed)
			}
			if strings.HasSuffix(chunk, "\n") {
				lines = append(lines, current.String())
				current.Reset()
			}
		}
		if err != nil {
			if current.Len() > 0 {
				lines = append(lines, current.String())
			}
			break
		}
	}

	return lines, nil
}

func parseLogFile(filePath string, maxEntries int) ([]LogEntry, error) {
	rawLines, err := readTail(filePath, maxTailBytes)
	if err != nil {
		return nil, err
	}

	if len(rawLines) > maxEntries*8 {
		rawLines = rawLines[len(rawLines)-(maxEntries*8):]
	}

	var entries []LogEntry
	baseName := filepath.Base(filePath)
	var current *LogEntry

	flush := func() {
		if current != nil {
			entries = append(entries, *current)
			current = nil
		}
	}

	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if entry, ok := parseJSONLine(trimmed, baseName); ok {
			flush()
			entries = append(entries, entry)
			continue
		}

		if entry, ok := parseLogfmtLine(trimmed, baseName); ok {
			flush()
			current = &entry
			continue
		}

		if isTextLogHeader(trimmed) {
			flush()
			current = &LogEntry{
				Level:   inferLogLevel(trimmed),
				Message: trimmed,
				Raw:     trimmed,
				Source:  baseName,
			}
			continue
		}

		if current == nil {
			current = &LogEntry{
				Level:   inferLogLevel(trimmed),
				Message: trimmed,
				Raw:     trimmed,
				Source:  baseName,
			}
			continue
		}

		current.Raw += "\n" + line
		if current.Stack == "" {
			current.Stack = line
		} else {
			current.Stack += "\n" + line
		}
		if strings.HasPrefix(trimmed, "panic:") || strings.HasPrefix(trimmed, "fatal error:") {
			current.Level = "PANIC"
		}
	}
	flush()

	if len(entries) > maxEntries {
		return entries[len(entries)-maxEntries:], nil
	}
	return entries, nil
}

func parseJSONLine(line, source string) (LogEntry, bool) {
	if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
		return LogEntry{}, false
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return LogEntry{}, false
	}

	entry := LogEntry{Raw: line, Source: source}

	entry.Level = strings.ToUpper(firstString(fields, "level", "lvl", "severity", "Level"))
	entry.Message = firstString(fields, "msg", "message", "Message")
	entry.Timestamp = firstString(fields, "time", "ts", "timestamp", "@timestamp")
	entry.Caller = firstString(fields, "caller", "Caller", "source")
	entry.Stack = firstString(fields, "stack", "stacktrace", "Stack")

	if entry.Caller == "" {
		if src, ok := fields["source"].(map[string]any); ok {
			file, _ := src["file"].(string)
			if lineNum, ok := src["line"].(float64); ok && file != "" {
				entry.Caller = fmt.Sprintf("%s:%d", file, int(lineNum))
			} else if file != "" {
				entry.Caller = file
			}
		}
	}

	if entry.Level == "" {
		entry.Level = inferLogLevel(line)
	}
	if entry.Message == "" {
		entry.Message = line
	}

	return entry, true
}

func parseLogfmtLine(line, source string) (LogEntry, bool) {
	if !logfmtRegex.MatchString(line) {
		return LogEntry{}, false
	}

	fields := parseLogfmtFields(line)
	if len(fields) == 0 {
		return LogEntry{}, false
	}

	entry := LogEntry{Raw: line, Source: source}
	entry.Level = strings.ToUpper(firstMapValue(fields, "level", "lvl", "severity"))
	entry.Message = firstMapValue(fields, "msg", "message")
	entry.Timestamp = firstMapValue(fields, "time", "ts", "timestamp")
	entry.Caller = firstMapValue(fields, "caller", "source")

	if entry.Level == "" {
		entry.Level = inferLogLevel(line)
	}
	if entry.Message == "" {
		entry.Message = line
	}

	return entry, true
}

func parseLogfmtFields(line string) map[string]string {
	fields := map[string]string{}

	i := 0
	for i < len(line) {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		start := i
		for i < len(line) && line[i] != '=' && line[i] != ' ' {
			i++
		}
		if i >= len(line) || line[i] != '=' {
			for i < len(line) && line[i] != ' ' {
				i++
			}
			continue
		}

		key := line[start:i]
		i++

		var value string
		if i < len(line) && line[i] == '"' {
			i++
			var sb strings.Builder
			for i < len(line) && line[i] != '"' {
				if line[i] == '\\' && i+1 < len(line) {
					i++
				}
				sb.WriteByte(line[i])
				i++
			}
			i++
			value = sb.String()
		} else {
			valueStart := i
			for i < len(line) && line[i] != ' ' {
				i++
			}
			value = line[valueStart:i]
		}

		if key != "" {
			fields[key] = value
		}
	}

	return fields
}

func firstString(fields map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := fields[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func firstMapValue(fields map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := fields[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func isTextLogHeader(line string) bool {
	return textTimestampRegex.MatchString(line)
}

func inferLogLevel(line string) string {
	match := levelRegex.FindString(strings.ToUpper(line))
	switch match {
	case "PANIC":
		return "PANIC"
	case "FATAL":
		return "FATAL"
	case "ERROR", "ERR":
		return "ERROR"
	case "WARN", "WARNING":
		return "WARN"
	case "DEBUG":
		return "DEBUG"
	case "TRACE":
		return "TRACE"
	case "INFO":
		return "INFO"
	}
	if strings.Contains(line, "panic:") {
		return "PANIC"
	}
	return "INFO"
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	var res []string
	for _, item := range s {
		if !seen[item] {
			seen[item] = true
			res = append(res, item)
		}
	}
	return res
}
