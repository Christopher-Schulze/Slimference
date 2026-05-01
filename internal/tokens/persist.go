package tokens

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const calibrationCap = 1000

type calibrationEntry struct {
	Model     string `json:"model"`
	Observed  int    `json:"observed"`
	Estimated int    `json:"estimated"`
	Ratio     int64  `json:"ratio"`
	Timestamp int64  `json:"ts"`
}

var (
	calMu   sync.Mutex
	calFile string
	calBuf  []calibrationEntry
)

func initCalibration(dir string) {
	calMu.Lock()
	defer calMu.Unlock()
	calFile = filepath.Join(dir, "anthropic.jsonl")
	calBuf = nil
	loadCalibrationLocked()
}

func loadCalibrationLocked() {
	if calFile == "" {
		return
	}
	data, err := os.ReadFile(calFile)
	if err != nil {
		return
	}
	lines := splitLines(string(data))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var e calibrationEntry
		if json.Unmarshal([]byte(line), &e) == nil && e.Model != "" {
			calBuf = append(calBuf, e)
		}
	}
	if len(calBuf) > calibrationCap {
		calBuf = calBuf[len(calBuf)-calibrationCap:]
	}
	replayCalibrationLocked()
}

func replayCalibrationLocked() {
	latest := map[string]int64{}
	for _, e := range calBuf {
		family := modelFamily(e.Model)
		if family == "" {
			continue
		}
		latest[family] = e.Ratio
	}
	for family, ratio := range latest {
		val, _ := anthropic.perModel.LoadOrStore(family, &modelRatio{})
		mr := val.(*modelRatio)
		mr.value.Store(ratio)
	}
}

func appendCalibration(model string, observed, estimated int, ratio int64) {
	calMu.Lock()
	defer calMu.Unlock()
	if calFile == "" {
		return
	}
	e := calibrationEntry{
		Model:     model,
		Observed:  observed,
		Estimated: estimated,
		Ratio:     ratio,
		Timestamp: time.Now().Unix(),
	}
	calBuf = append(calBuf, e)
	if len(calBuf) > calibrationCap {
		calBuf = calBuf[len(calBuf)-calibrationCap:]
	}
	line, _ := json.Marshal(e)
	_ = os.MkdirAll(filepath.Dir(calFile), 0700)
	f, err := os.OpenFile(calFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	f.Write(line)
	f.Write([]byte{'\n'})
	f.Close()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func LoadCalibrationFromDir(dir string) {
	initCalibration(dir)
}

func ResetCalibration() {
	calMu.Lock()
	defer calMu.Unlock()
	calFile = ""
	calBuf = nil
}
