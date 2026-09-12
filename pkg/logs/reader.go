package logs

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func FindLogFiles(rootDir string) []string {
	var files []string

	candidates := []string{
		"app.log", "server.log", "debug.log", "error.log",
		"logs/app.log", "logs/error.log", "logs/server.log", "logs/debug.log",
	}

	for _, c := range candidates {
		p := filepath.Join(rootDir, c)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			files = append(files, p)
		}
	}

	logsDir := filepath.Join(rootDir, "logs")
	if fi, err := os.Stat(logsDir); err == nil && fi.IsDir() {
		_ = filepath.Walk(logsDir, func(path string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() && strings.HasSuffix(path, ".log") {
				files = append(files, path)
			}
			return nil
		})
	}

	return uniqueStrings(files)
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

func parseLogFile(filePath string, maxEntries int) ([]LogEntry, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rawLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		rawLines = append(rawLines, scanner.Text())
	}

	if len(rawLines) > maxEntries*4 {
		rawLines = rawLines[len(rawLines)-(maxEntries*4):]
	}

	var entries []LogEntry
	baseName := filepath.Base(filePath)

	var currentEntry *LogEntry

	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			var jsonMap map[string]any
			if err := json.Unmarshal([]byte(trimmed), &jsonMap); err == nil {
				if currentEntry != nil {
					entries = append(entries, *currentEntry)
					currentEntry = nil
				}

				entry := LogEntry{
					Raw:    trimmed,
					Source: baseName,
				}

				if lvl, ok := jsonMap["level"].(string); ok {
					entry.Level = strings.ToUpper(lvl)
				} else if lvl, ok := jsonMap["lvl"].(string); ok {
					entry.Level = strings.ToUpper(lvl)
				}

				if msg, ok := jsonMap["msg"].(string); ok {
					entry.Message = msg
				} else if msg, ok := jsonMap["message"].(string); ok {
					entry.Message = msg
				}

				if ts, ok := jsonMap["time"].(string); ok {
					entry.Timestamp = ts
				} else if ts, ok := jsonMap["ts"].(string); ok {
					entry.Timestamp = ts
				}

				if caller, ok := jsonMap["caller"].(string); ok {
					entry.Caller = caller
				}
				if stack, ok := jsonMap["stack"].(string); ok {
					entry.Stack = stack
				}

				entries = append(entries, entry)
				continue
			}
		}

		isNewEntry := isTextLogHeader(trimmed)

		if isNewEntry {
			if currentEntry != nil {
				entries = append(entries, *currentEntry)
			}
			level := inferLogLevel(trimmed)
			currentEntry = &LogEntry{
				Level:   level,
				Message: trimmed,
				Raw:     trimmed,
				Source:  baseName,
			}
		} else {
			if currentEntry != nil {
				currentEntry.Raw += "\n" + line
				if currentEntry.Stack == "" {
					currentEntry.Stack = line
				} else {
					currentEntry.Stack += "\n" + line
				}
			} else {
				currentEntry = &LogEntry{
					Level:   "INFO",
					Message: trimmed,
					Raw:     trimmed,
					Source:  baseName,
				}
			}
		}
	}

	if currentEntry != nil {
		entries = append(entries, *currentEntry)
	}

	if len(entries) > maxEntries {
		return entries[len(entries)-maxEntries:], nil
	}

	return entries, nil
}

func isTextLogHeader(line string) bool {
	if len(line) >= 19 && line[4] == '/' && line[7] == '/' && line[10] == ' ' && line[13] == ':' {
		return true
	}

	if len(line) >= 10 && line[4] == '-' && line[7] == '-' {
		return true
	}
	return false
}

func inferLogLevel(line string) string {
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, "PANIC"):
		return "PANIC"
	case strings.Contains(upper, "FATAL"):
		return "FATAL"
	case strings.Contains(upper, "ERROR"):
		return "ERROR"
	case strings.Contains(upper, "WARN"):
		return "WARN"
	case strings.Contains(upper, "DEBUG"):
		return "DEBUG"
	default:
		return "INFO"
	}
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
