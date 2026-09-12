package logs

import (
	"regexp"
	"strings"
)

var panicLocationRegex = regexp.MustCompile(`(?m)([a-zA-Z0-9_\-\./]+\.go):([0-9]+)`)

type LastErrorInfo struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	File       string `json:"file,omitempty"`
	Line       string `json:"line,omitempty"`
	StackTrace string `json:"stack_trace,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	Source     string `json:"source,omitempty"`
}

func FindLastError(rootDir string) (*LastErrorInfo, error) {
	entries, err := ReadEntries(rootDir, 100)
	if err != nil {
		return nil, err
	}

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Level == "ERROR" || e.Level == "PANIC" || e.Level == "FATAL" || strings.Contains(strings.ToLower(e.Raw), "panic:") {
			info := &LastErrorInfo{
				Type:       "error",
				Message:    e.Message,
				StackTrace: e.Stack,
				Timestamp:  e.Timestamp,
				Source:     e.Source,
			}

			if strings.Contains(strings.ToLower(e.Raw), "panic:") {
				info.Type = "panic"
			}

			if e.Caller != "" {
				parts := strings.Split(e.Caller, ":")
				if len(parts) >= 1 {
					info.File = parts[0]
				}
				if len(parts) >= 2 {
					info.Line = parts[1]
				}
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

			return info, nil
		}
	}

	return &LastErrorInfo{
		Type:    "none",
		Message: "No recent backend errors or panics found in application logs",
	}, nil
}
