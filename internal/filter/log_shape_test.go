package filter

import (
	"strings"
	"testing"
)

func TestDetectLogShape_ISO8601(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("2024-01-15T10:00:00Z INFO request processed\n")
	}
	shape, conf := DetectLogShape([]byte(sb.String()))
	if shape != LogShapeISO8601 || conf < 0.6 {
		t.Fatalf("iso8601: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_UnixTimestamp(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("1705312800.123456 INFO request ok\n")
	}
	shape, conf := DetectLogShape([]byte(sb.String()))
	if shape != LogShapeUnixTimestamp || conf < 0.6 {
		t.Fatalf("unix: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_Syslog(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("Jan 15 10:00:00 myhost myapp[1234]: something happened\n")
	}
	shape, conf := DetectLogShape([]byte(sb.String()))
	if shape != LogShapeSyslog || conf < 0.6 {
		t.Fatalf("syslog: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_Bracketed(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("[INFO] request processed successfully\n")
	}
	shape, conf := DetectLogShape([]byte(sb.String()))
	if shape != LogShapeBracketedLevel || conf < 0.5 {
		t.Fatalf("bracketed: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_JSONLines(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString(`{"level":"info","msg":"request processed","ts":1705312800}` + "\n")
	}
	shape, conf := DetectLogShape([]byte(sb.String()))
	if shape != LogShapeJSONLines || conf < 0.5 {
		t.Fatalf("jsonlines: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_SeverityFallback(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("INFO something happened\n")
	}
	shape, conf := DetectLogShape([]byte(sb.String()))
	if shape != LogShapeBracketedLevel || conf < 0.3 {
		t.Fatalf("severity fallback: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_None(t *testing.T) {
	t.Parallel()
	input := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	shape, conf := DetectLogShape([]byte(input))
	if shape != LogShapeNone {
		t.Fatalf("random code: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_Empty(t *testing.T) {
	t.Parallel()
	shape, conf := DetectLogShape(nil)
	if shape != LogShapeNone || conf != 0 {
		t.Fatalf("empty: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_OnlyBlankLines(t *testing.T) {
	t.Parallel()
	shape, conf := DetectLogShape([]byte("\n\n\n"))
	if shape != LogShapeNone || conf != 0 {
		t.Fatalf("blank: shape=%v conf=%.2f", shape, conf)
	}
}

func TestDetectLogShape_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		shape LogShape
		want  string
	}{
		{LogShapeNone, "none"},
		{LogShapeISO8601, "iso8601"},
		{LogShapeUnixTimestamp, "unix"},
		{LogShapeSyslog, "syslog"},
		{LogShapeBracketedLevel, "bracketed"},
		{LogShapeJSONLines, "jsonlines"},
		{LogShape(99), "none"},
	}
	for _, tt := range tests {
		if got := tt.shape.String(); got != tt.want {
			t.Errorf("LogShape(%d).String() = %q, want %q", tt.shape, got, tt.want)
		}
	}
}

func TestDetectLogShape_MixedBelowThreshold(t *testing.T) {
	t.Parallel()
	input := "some random line\n2024-01-15T10:00:00Z log entry\nanother random\nyet another\n"
	shape, conf := DetectLogShape([]byte(input))
	if shape != LogShapeNone {
		t.Fatalf("mixed below threshold: shape=%v conf=%.2f", shape, conf)
	}
}
