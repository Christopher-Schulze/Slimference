// Package security provides secret detection and redaction for LLM proxy requests.
package security

import (
	"fmt"
	"math"
	"regexp"
)

// SecretPattern describes a named regex-based secret detection rule with an optional
// minimum Shannon entropy threshold. Patterns with MinEntropy == 0 match on regex alone.
type SecretPattern struct {
	Name       string
	Regex      *regexp.Regexp
	MinEntropy float64
}

// DefaultPatterns is the built-in set of secret detection rules applied to all messages
// unless overridden. Patterns are evaluated in order; the first match per text segment wins.
var DefaultPatterns = []SecretPattern{
	{
		Name:  "AWS Access Key",
		Regex: regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	},
	{
		Name:  "AWS Secret Key",
		Regex: regexp.MustCompile(`(?i)aws_secret_access_key\s*=\s*\S+`),
	},
	{
		Name:  "GitHub Token",
		Regex: regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
	},
	{
		Name:  "GitHub Fine-grained Token",
		Regex: regexp.MustCompile(`github_pat_[a-zA-Z0-9_]{82}`),
	},
	{
		Name:  "Anthropic API Key",
		Regex: regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-]{90,}`),
	},
	{
		Name:  "OpenAI API Key",
		Regex: regexp.MustCompile(`sk-[a-zA-Z0-9]{48,}`),
	},
	{
		Name:  "Stripe Key",
		Regex: regexp.MustCompile(`(?:sk|pk)_(?:test|live)_[a-zA-Z0-9]{24,}`),
	},
	{
		Name:  "Generic Bearer Token",
		Regex: regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_.~+/=\-]{20,}`),
	},
	{
		Name:  "Password in Config",
		Regex: regexp.MustCompile(`(?i)(?:password|passwd|secret|token)\s*[:=]\s*["']?\S{8,}`),
	},
	{
		Name:  "Env Secret Value",
		Regex: regexp.MustCompile(`(?i)(?:DATABASE_URL|REDIS_URL|SECRET_KEY|PRIVATE_KEY)\s*=\s*\S+`),
	},
	{
		Name:  "RSA Private Key",
		Regex: regexp.MustCompile(`-----BEGIN (?:RSA )?PRIVATE KEY-----`),
	},
	{
		Name:  "SSH Private Key",
		Regex: regexp.MustCompile(`-----BEGIN OPENSSH PRIVATE KEY-----`),
	},
	{
		Name:       "High-Entropy String",
		Regex:      regexp.MustCompile(`[a-zA-Z0-9+/=]{40,}`),
		MinEntropy: 4.5,
	},
}

// CompilePattern compiles a custom secret pattern from a name and regex string.
// Returns an error if the regex is invalid.
func CompilePattern(name, pattern string) (SecretPattern, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return SecretPattern{}, fmt.Errorf("compile pattern %q: %w", name, err)
	}
	return SecretPattern{Name: name, Regex: re}, nil
}

// shannonEntropy computes the Shannon entropy (in bits) of the string s.
// Returns 0 for empty strings.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]int, 64)
	for _, ch := range s {
		freq[ch]++
	}
	n := float64(len([]rune(s)))
	entropy := 0.0
	for _, count := range freq {
		p := float64(count) / n
		entropy -= p * math.Log2(p)
	}
	return entropy
}
