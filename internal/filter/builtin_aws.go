package filter

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
)

// TryCompactAwsJSON strips nested AWS ResponseMetadata from JSON stdout when argv is `aws` (F16 partial).
func TryCompactAwsJSON(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "aws" && b != "aws.exe" {
		return stdout, false
	}
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return stdout, false
	}
	var v interface{}
	_ = json.Unmarshal(trimmed, &v)
	v = stripAWSResponseMetadataValue(v)
	out, _ := json.Marshal(v)
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

// awsJSONStripKeys are common AWS SDK JSON envelopes removed when they shrink output (F16 partial).
var awsJSONStripKeys = []string{
	"ResponseMetadata",
	"ResultMetadata",
	"SdkHttpMetadata",
}

func stripAWSResponseMetadataValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for _, k := range awsJSONStripKeys {
			delete(t, k)
		}
		for k, val := range t {
			t[k] = stripAWSResponseMetadataValue(val)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = stripAWSResponseMetadataValue(val)
		}
		return t
	default:
		return v
	}
}
