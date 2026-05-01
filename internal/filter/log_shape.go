package filter

import (
	"encoding/json"
	"regexp"
	"strings"
)

type LogShape int

const (
	LogShapeNone LogShape = iota
	LogShapeISO8601
	LogShapeUnixTimestamp
	LogShapeSyslog
	LogShapeBracketedLevel
	LogShapeJSONLines
)

func (s LogShape) String() string {
	switch s {
	case LogShapeISO8601:
		return "iso8601"
	case LogShapeUnixTimestamp:
		return "unix"
	case LogShapeSyslog:
		return "syslog"
	case LogShapeBracketedLevel:
		return "bracketed"
	case LogShapeJSONLines:
		return "jsonlines"
	default:
		return "none"
	}
}

var (
	reISO8601   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}`)
	reUnixEpoch = regexp.MustCompile(`^\d{9,10}\.`)
	reSyslog    = regexp.MustCompile(`^[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}`)
	reBracketed = regexp.MustCompile(`^\[[A-Z]+\]`)
)

const shapeSampleLines = 50

type shapeDet struct {
	shape      LogShape
	confidence float64
}

func DetectLogShape(stdout []byte) (LogShape, float64) {
	if len(stdout) == 0 {
		return LogShapeNone, 0
	}
	lines := strings.Split(string(stdout), "\n")
	if len(lines) > shapeSampleLines {
		lines = lines[:shapeSampleLines]
	}

	nonEmpty := 0
	iso, unix, syslog, bracket, jsonl := 0, 0, 0, 0, 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		nonEmpty++
		if reISO8601.MatchString(t) {
			iso++
		}
		if reUnixEpoch.MatchString(t) {
			unix++
		}
		if reSyslog.MatchString(t) {
			syslog++
		}
		if reBracketed.MatchString(t) {
			bracket++
		}
		if isJSONLine(t) {
			jsonl++
		}
	}
	if nonEmpty == 0 {
		return LogShapeNone, 0
	}

	var best shapeDet
	check := func(shape LogShape, count int, threshold float64) {
		conf := float64(count) / float64(nonEmpty)
		if conf >= threshold && conf > best.confidence {
			best = shapeDet{shape: shape, confidence: conf}
		}
	}
	check(LogShapeISO8601, iso, 0.6)
	check(LogShapeUnixTimestamp, unix, 0.6)
	check(LogShapeSyslog, syslog, 0.6)
	check(LogShapeBracketedLevel, bracket, 0.5)
	check(LogShapeJSONLines, jsonl, 0.5)

	if best.confidence >= 0.5 {
		return best.shape, best.confidence
	}

	severityCount := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if hasSeverityToken(t) {
			severityCount++
		}
	}
	severityConf := float64(severityCount) / float64(nonEmpty)
	if severityConf >= 0.3 && severityConf > best.confidence {
		return LogShapeBracketedLevel, severityConf
	}

	return LogShapeNone, 0
}

var severityTokens = []string{"INFO", "WARN", "ERROR", "DEBUG", "TRACE", "FATAL"}

func hasSeverityToken(line string) bool {
	for _, tok := range severityTokens {
		if strings.Contains(line, tok) {
			return true
		}
	}
	return false
}

func isJSONLine(s string) bool {
	if len(s) < 2 || s[0] != '{' {
		return false
	}
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}
