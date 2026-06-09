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

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

type promptCacheRateBucket struct {
	windowStart time.Time
	count       int
}

const openAIPromptCacheRejectTTL = 30 * time.Minute
const openAIPromptCacheNetMinNegativeSamples = 2
const openAIPromptCacheNetMinLossTokens = 1024

type promptCacheNetBucket struct {
	readTokens      int
	createTokens    int
	samples         int
	negativeSamples int
}

type openAIPromptCacheDecision struct {
	Applied            bool
	Key                string
	Retention          string
	Reason             string
	StablePrefixHash   string
	StablePrefixTokens int
}

type openAIStablePrefixPlan struct {
	Detected bool
	Hash     string
	Tokens   int
	Reason   string
}

func (p *Proxy) injectOpenAIPromptCache(provider types.Provider, body []byte, model string, inputTokens int, sessionID string) ([]byte, openAIPromptCacheDecision) {
	cfg := p.config.Proxy.OpenAIPromptCache
	if !cfg.Enabled {
		return body, openAIPromptCacheDecision{Reason: "disabled"}
	}
	if provider != types.OpenAI {
		return body, openAIPromptCacheDecision{Reason: "unsupported_provider"}
	}
	now := time.Now()
	if p.openAIPromptCacheRejected(provider, model, now) {
		return body, openAIPromptCacheDecision{Reason: "rejected_cooldown"}
	}
	caps := types.CapabilitiesFor(provider)
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, openAIPromptCacheDecision{Reason: "invalid_json"}
	}
	plan := planOpenAIStablePrefixFromRoot(root)
	if plan.Hash == "" {
		return body, openAIPromptCacheDecision{
			Reason:             plan.Reason,
			StablePrefixTokens: plan.Tokens,
		}
	}
	if plan.Tokens < cfg.MinTokens {
		return body, openAIPromptCacheDecision{
			Reason:             "stable_prefix_below_min_tokens",
			StablePrefixHash:   plan.Hash,
			StablePrefixTokens: plan.Tokens,
		}
	}
	changed := false
	decision := openAIPromptCacheDecision{
		StablePrefixHash:   plan.Hash,
		StablePrefixTokens: plan.Tokens,
	}
	if _, exists := root["prompt_cache_key"]; !exists && caps.SupportsPromptCacheKey {
		key := buildOpenAIPromptCacheKey(cfg, model, sessionID, plan.Hash)
		if key != "" {
			if p.openAIPromptCacheKeyRejected(provider, model, key, now) {
				decision.Reason = "negative_net_cooldown"
			} else if p.allowOpenAIPromptCacheKey(key, cfg.MaxRequestsPerKeyPerMinute, now) {
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
			decision.Reason = "caller_owned"
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

func buildOpenAIPromptCacheKey(cfg config.OpenAIPromptCacheConfig, model string, sessionID string, stablePrefixHash string) string {
	strategy := strings.TrimSpace(cfg.PromptCacheKeyStrategy)
	if strategy == "" {
		strategy = "model_stable_prefix"
	}
	stablePrefixHash = strings.TrimSpace(stablePrefixHash)
	switch strategy {
	case "off":
		return ""
	case "static":
		return strings.TrimSpace(cfg.StaticPromptCacheKey)
	case "stable_prefix":
		return hashedPromptCacheKey("stable_prefix", stablePrefixHash)
	case "model_stable_prefix":
		return hashedPromptCacheKey("model_stable_prefix", joinKeyParts(model, stablePrefixHash))
	case "session":
		return hashedPromptCacheKey("session", joinKeyParts(sessionID, stablePrefixHash))
	case "model_session":
		return hashedPromptCacheKey("model_session", joinKeyParts(model, sessionID, stablePrefixHash))
	default:
		return ""
	}
}

func joinKeyParts(parts ...string) string {
	var kept []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\x00")
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

func (p *Proxy) markOpenAIPromptCacheRejected(provider types.Provider, model string, now time.Time) {
	key := openAIPromptCacheRejectKey(provider, model)
	if key == "" {
		return
	}
	p.openAIPromptCacheMu.Lock()
	defer p.openAIPromptCacheMu.Unlock()
	if p.openAIPromptCacheRejects == nil {
		p.openAIPromptCacheRejects = make(map[string]time.Time)
	}
	p.openAIPromptCacheRejects[key] = now
}

func (p *Proxy) observeOpenAIPromptCacheNet(provider types.Provider, model string, decision openAIPromptCacheDecision, usage cacheUsage, now time.Time) {
	if !decision.Applied || decision.Key == "" {
		return
	}
	key := openAIPromptCacheKeyRejectKey(provider, model, decision.Key)
	if key == "" {
		return
	}
	readTokens := usage.ReadTokens
	createTokens := usage.CreateTokens
	if readTokens == 0 && createTokens == 0 {
		return
	}
	p.openAIPromptCacheMu.Lock()
	defer p.openAIPromptCacheMu.Unlock()
	if p.openAIPromptCacheNet == nil {
		p.openAIPromptCacheNet = make(map[string]promptCacheNetBucket)
	}
	bucket := p.openAIPromptCacheNet[key]
	bucket.samples++
	bucket.readTokens += readTokens
	bucket.createTokens += createTokens
	if readTokens-createTokens < 0 {
		bucket.negativeSamples++
	}
	p.openAIPromptCacheNet[key] = bucket
	net := bucket.readTokens - bucket.createTokens
	if bucket.negativeSamples >= openAIPromptCacheNetMinNegativeSamples &&
		net <= -openAIPromptCacheNetMinLossTokens {
		if p.openAIPromptCacheRejects == nil {
			p.openAIPromptCacheRejects = make(map[string]time.Time)
		}
		p.openAIPromptCacheRejects[key] = now
		delete(p.openAIPromptCacheNet, key)
	}
}

func (p *Proxy) openAIPromptCacheRejected(provider types.Provider, model string, now time.Time) bool {
	key := openAIPromptCacheRejectKey(provider, model)
	if key == "" {
		return false
	}
	p.openAIPromptCacheMu.Lock()
	defer p.openAIPromptCacheMu.Unlock()
	rejectedAt, ok := p.openAIPromptCacheRejects[key]
	if !ok {
		return false
	}
	if now.Sub(rejectedAt) >= openAIPromptCacheRejectTTL {
		delete(p.openAIPromptCacheRejects, key)
		return false
	}
	return true
}

func (p *Proxy) openAIPromptCacheKeyRejected(provider types.Provider, model string, promptCacheKey string, now time.Time) bool {
	key := openAIPromptCacheKeyRejectKey(provider, model, promptCacheKey)
	if key == "" {
		return false
	}
	p.openAIPromptCacheMu.Lock()
	defer p.openAIPromptCacheMu.Unlock()
	rejectedAt, ok := p.openAIPromptCacheRejects[key]
	if !ok {
		return false
	}
	if now.Sub(rejectedAt) >= openAIPromptCacheRejectTTL {
		delete(p.openAIPromptCacheRejects, key)
		return false
	}
	return true
}

func openAIPromptCacheRejectKey(provider types.Provider, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return provider.String()
	}
	return provider.String() + "\x00" + model
}

func openAIPromptCacheKeyRejectKey(provider types.Provider, model string, promptCacheKey string) string {
	promptCacheKey = strings.TrimSpace(promptCacheKey)
	if promptCacheKey == "" {
		return ""
	}
	return openAIPromptCacheRejectKey(provider, model) + "\x00key\x00" + promptCacheKey
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

func planOpenAIStablePrefix(body []byte) openAIStablePrefixPlan {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return openAIStablePrefixPlan{Reason: "invalid_json"}
	}
	return planOpenAIStablePrefixFromRoot(root)
}

func planOpenAIStablePrefixFromRoot(root map[string]json.RawMessage) openAIStablePrefixPlan {
	var parts [][]byte
	var tokens int
	for _, key := range []string{"instructions", "system", "developer"} {
		if raw, ok := root[key]; ok {
			part := compactRawJSON(raw)
			parts = append(parts, []byte(key), part)
			tokens += estimateTokensFromText(string(part))
		}
	}
	if raw, ok := root["tools"]; ok {
		part := compactRawJSON(raw)
		parts = append(parts, []byte("tools"), part)
		tokens += estimateTokensFromText(string(part))
	}
	if raw, ok := root["messages"]; ok {
		if prefix, ok := stablePrefixArray(raw); ok {
			part := compactRawJSON(mustMarshalRawArray(prefix))
			parts = append(parts, []byte("messages"), part)
			tokens += estimateTokensFromText(string(part))
		}
	}
	if raw, ok := root["input"]; ok {
		if prefix, ok := stablePrefixArray(raw); ok {
			part := compactRawJSON(mustMarshalRawArray(prefix))
			parts = append(parts, []byte("input"), part)
			tokens += estimateTokensFromText(string(part))
		}
	}
	if len(parts) == 0 {
		return openAIStablePrefixPlan{Detected: true, Reason: "no_stable_prefix"}
	}
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write(part)
		_, _ = h.Write([]byte{0})
	}
	return openAIStablePrefixPlan{
		Detected: true,
		Hash:     hex.EncodeToString(h.Sum(nil)[:12]),
		Tokens:   tokens,
		Reason:   "planned",
	}
}

func stablePrefixArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, false
	}
	lastUser := -1
	for i := len(arr) - 1; i >= 0; i-- {
		if rawMessageRole(arr[i]) == "user" {
			lastUser = i
			break
		}
	}
	if lastUser <= 0 {
		return nil, false
	}
	return arr[:lastUser], true
}

func rawMessageRole(raw json.RawMessage) string {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil {
		return ""
	}
	var role string
	_ = json.Unmarshal(entry["role"], &role)
	return role
}

func compactRawJSON(raw json.RawMessage) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

func mustMarshalRawArray(arr []json.RawMessage) []byte {
	out, err := json.Marshal(arr)
	if err != nil {
		return nil
	}
	return out
}
