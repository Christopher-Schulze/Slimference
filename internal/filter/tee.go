package filter

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteTeeRecovery writes raw stdout/stderr to ~/.tokenproxy/tee for debugging when the subprocess fails.
func WriteTeeRecovery(teeDir string, rawStdout, rawStderr []byte) (path string, err error) {
	if err := os.MkdirAll(teeDir, 0755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("tee-%d.txt", time.Now().UnixNano())
	path = filepath.Join(teeDir, name)
	var b bytes.Buffer
	b.WriteString("--- stdout ---\n")
	b.Write(rawStdout)
	b.WriteString("\n--- stderr ---\n")
	b.Write(rawStderr)
	b.WriteByte('\n')
	return path, os.WriteFile(path, b.Bytes(), 0644)
}
