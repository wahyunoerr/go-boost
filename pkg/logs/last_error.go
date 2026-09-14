package logs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var panicLocationRegex = regexp.MustCompile(`([a-zA-Z0-9_\-./]+\.go):([0-9]+)`)

type LastErrorInfo struct {
	Type         string `json:"type"`
	Message      string `json:"message"`
	File         string `json:"file,omitempty"`
	Line         string `json:"line,omitempty"`
	StackTrace   string `json:"stack_trace,omitempty"`
	Timestamp    string `json:"timestamp,omitempty"`
	Source       string `json:"source,omitempty"`
	Snippet      string `json:"snippet,omitempty"`
	RootCause    string `json:"root_cause,omitempty"`
	SuggestedFix string `json:"suggested_fix,omitempty"`
}

func FindLastError(rootDir string) (*LastErrorInfo, error) {
	if rootDir == "" {
		rootDir = "."
	}

	saved, savedAt := loadSavedReport(rootDir)
	fromLogs, logAt, err := findErrorInLogs(rootDir)
	if err != nil && saved == nil {
		return nil, err
	}

	switch {
	case saved != nil && fromLogs != nil:
		if logAt.After(savedAt) {
			return fromLogs, nil
		}
		return saved, nil
	case saved != nil:
		return saved, nil
	case fromLogs != nil:
		return fromLogs, nil
	}

	return &LastErrorInfo{
		Type:    "none",
		Message: "No recent backend errors or panics found in application logs",
	}, nil
}

func loadSavedReport(rootDir string) (*LastErrorInfo, time.Time) {
	savedPath := filepath.Join(rootDir, ".go-boost", "last_error.json")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		return nil, time.Time{}
	}

	var rep struct {
		Category     string `json:"category"`
		Title        string `json:"title"`
		Message      string `json:"message"`
		File         string `json:"file"`
		Line         int    `json:"line"`
		Snippet      string `json:"snippet"`
		RootCause    string `json:"root_cause"`
		SuggestedFix string `json:"suggested_fix"`
		Timestamp    string `json:"timestamp"`
		RawOutput    string `json:"raw_output"`
	}
	if err := json.Unmarshal(data, &rep); err != nil || rep.Message == "" {
		return nil, time.Time{}
	}

	lineStr := ""
	if rep.Line > 0 {
		lineStr = fmt.Sprintf("%d", rep.Line)
	}

	savedAt := parseLogTimestamp(rep.Timestamp)
	if savedAt.IsZero() {
		if fi, err := os.Stat(savedPath); err == nil {
			savedAt = fi.ModTime()
		}
	}

	return &LastErrorInfo{
		Type:         rep.Category,
		Message:      rep.Message,
		File:         rep.File,
		Line:         lineStr,
		StackTrace:   rep.RawOutput,
		Timestamp:    rep.Timestamp,
		Source:       ".go-boost/last_error.json",
		Snippet:      rep.Snippet,
		RootCause:    rep.RootCause,
		SuggestedFix: rep.SuggestedFix,
	}, savedAt
}

func findErrorInLogs(rootDir string) (*LastErrorInfo, time.Time, error) {
	entries, err := ReadEntries(rootDir, 200)
	if err != nil {
		return nil, time.Time{}, err
	}

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		lowerRaw := strings.ToLower(e.Raw)
		isError := e.Level == "ERROR" || e.Level == "PANIC" || e.Level == "FATAL" ||
			strings.Contains(lowerRaw, "panic:") || strings.Contains(lowerRaw, "fatal error:")
		if !isError {
			continue
		}

		info := &LastErrorInfo{
			Type:       "error",
			Message:    e.Message,
			StackTrace: e.Stack,
			Timestamp:  e.Timestamp,
			Source:     e.Source,
		}
		if strings.Contains(lowerRaw, "panic:") || strings.Contains(lowerRaw, "fatal error:") {
			info.Type = "panic"
		}

		if e.Caller != "" {
			file, line := splitCaller(e.Caller)
			info.File = file
			info.Line = line
		}

		if info.File == "" {
			sourceText := e.Stack
			if sourceText == "" {
				sourceText = e.Raw
			}
			if match := panicLocationRegex.FindStringSubmatch(sourceText); len(match) > 2 {
				info.File = match[1]
				info.Line = match[2]
			}
		}

		return info, parseLogTimestamp(e.Timestamp), nil
	}

	return nil, time.Time{}, nil
}

func splitCaller(caller string) (string, string) {
	idx := strings.LastIndex(caller, ":")
	if idx <= 0 || idx == len(caller)-1 {
		return caller, ""
	}
	line := caller[idx+1:]
	for _, r := range line {
		if r < '0' || r > '9' {
			return caller, ""
		}
	}
	return caller[:idx], line
}

func parseLogTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z0700",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}
