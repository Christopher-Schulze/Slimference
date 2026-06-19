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
	if !isAWSCLIArgv(argv) {
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

// TryCompactAwsJSONExact exact-minifies AWS CLI JSON while preserving every
// original field, including ResponseMetadata. It is used by WSS stateful-safe
// paths where lossy metadata stripping is not allowed.
func TryCompactAwsJSONExact(argv []string, stdout []byte) ([]byte, bool) {
	if !isAWSCLIArgv(argv) {
		return stdout, false
	}
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return stdout, true
	}
	var buf bytes.Buffer
	buf.Grow(len(trimmed))
	if err := json.Compact(&buf, trimmed); err != nil {
		return stdout, true
	}
	compact := buf.Bytes()
	if len(compact) < len(stdout) {
		return compact, true
	}
	return stdout, true
}

func isAWSCLIArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	return b == "aws" || b == "aws.exe"
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
