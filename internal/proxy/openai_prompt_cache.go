package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

type promptCacheRateBucket struct {
	windowStart time.Time
	count       int
}

type openAIPromptCacheDecision struct {
	Applied   bool
	Key       string
	Retention string
	Reason    string
}

func (p *Proxy) injectOpenAIPromptCache(provider types.Provider, body []byte, model string, inputTokens int, sessionID string) ([]byte, openAIPromptCacheDecision) {
	cfg := p.config.Proxy.OpenAIPromptCache
	if !cfg.Enabled {
		return body, openAIPromptCacheDecision{Reason: "disabled"}
	}
	if provider != types.OpenAI {
		return body, openAIPromptCacheDecision{Reason: "unsupported_provider"}
	}
	caps := types.CapabilitiesFor(provider)
	if inputTokens < cfg.MinTokens {
		return body, openAIPromptCacheDecision{Reason: "below_min_tokens"}
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, openAIPromptCacheDecision{Reason: "invalid_json"}
	}
	changed := false
	decision := openAIPromptCacheDecision{}
	if _, exists := root["prompt_cache_key"]; !exists && caps.SupportsPromptCacheKey {
		key := buildOpenAIPromptCacheKey(cfg, model, sessionID)
		if key != "" {
			if p.allowOpenAIPromptCacheKey(key, cfg.MaxRequestsPerKeyPerMinute, time.Now()) {
				raw, _ := json.Marshal(key)
				root["prompt_cache_key"] = raw
				decision.Key = key
				changed = true
			} else {
				decision.Reason = "rate_limited"
			}
		}
	}
	if _, exists := root["prompt_cache_retention"]; !exists && caps.SupportsPromptCacheRetention {
		if retention := resolveOpenAIPromptCacheRetention(cfg.Retention, model); retention != "" {
			raw, _ := json.Marshal(retention)
			root["prompt_cache_retention"] = raw
			decision.Retention = retention
			changed = true
		}
	}
	if !changed {
		if decision.Reason == "" {
			decision.Reason = "no_change"
		}
		return body, decision
	}
	out, _ := json.Marshal(root)
	decision.Applied = true
	if decision.Reason == "" {
		decision.Reason = "applied"
	}
	return out, decision
}

func buildOpenAIPromptCacheKey(cfg config.OpenAIPromptCacheConfig, model string, sessionID string) string {
	strategy := strings.TrimSpace(cfg.PromptCacheKeyStrategy)
	if strategy == "" {
		strategy = "session"
	}
	switch strategy {
	case "off":
		return ""
	case "static":
		return strings.TrimSpace(cfg.StaticPromptCacheKey)
	case "session":
		return hashedPromptCacheKey("session", sessionID)
	case "model_session":
		return hashedPromptCacheKey("model_session", model+"\x00"+sessionID)
	default:
		return ""
	}
}

func hashedPromptCacheKey(prefix string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "slimference_" + prefix + "_" + hex.EncodeToString(sum[:12])
}

func resolveOpenAIPromptCacheRetention(retention string, model string) string {
	switch strings.TrimSpace(retention) {
	case "in_memory":
		return "in_memory"
	case "24h":
		if openAIModelSupportsExtendedPromptCache(model) {
			return "24h"
		}
		return ""
	default:
		return ""
	}
}

func openAIModelSupportsExtendedPromptCache(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "gpt-5.1") ||
		strings.HasPrefix(m, "gpt-5") ||
		strings.HasPrefix(m, "gpt-4.1")
}

func (p *Proxy) allowOpenAIPromptCacheKey(key string, maxPerMinute int, now time.Time) bool {
	if key == "" || maxPerMinute <= 0 {
		return true
	}
	p.openAIPromptCacheMu.Lock()
	defer p.openAIPromptCacheMu.Unlock()
	if p.openAIPromptCacheRate == nil {
		p.openAIPromptCacheRate = make(map[string]promptCacheRateBucket)
	}
	b := p.openAIPromptCacheRate[key]
	if b.windowStart.IsZero() || now.Sub(b.windowStart) >= time.Minute {
		p.openAIPromptCacheRate[key] = promptCacheRateBucket{windowStart: now, count: 1}
		return true
	}
	if b.count >= maxPerMinute {
		return false
	}
	b.count++
	p.openAIPromptCacheRate[key] = b
	return true
}

func peekPromptCacheUnsupportedError(resp *http.Response) bool {
	if resp == nil || resp.StatusCode < 400 || resp.StatusCode >= 500 {
		return false
	}
	const maxPeek = 64 * 1024
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxPeek))
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(buf))
	lower := strings.ToLower(string(buf))
	return strings.Contains(lower, "prompt_cache_key") || strings.Contains(lower, "prompt_cache_retention")
}
