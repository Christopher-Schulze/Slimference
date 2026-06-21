package proxy

import (
	"log/slog"
	"os"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/readcache"
	"github.com/Christopher-Schulze/Slimference/internal/toolprune"
	"github.com/Christopher-Schulze/Slimference/internal/toolusecache"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// GetAnalytics returns a snapshot of the current analytics state.
func (p *Proxy) GetAnalytics() analytics.AnalyticsSnapshot {
	return p.analytics.Snapshot()
}

// GetRecentRequests returns the last n request metrics from the ring buffer.
func (p *Proxy) GetRecentRequests(n int) []types.RequestMetrics {
	return p.analytics.RecentRequests(n)
}

// FlushCaches invalidates all in-memory caches.
func (p *Proxy) FlushCaches() {
	p.responseCache.Flush()
	p.layer1.Reset()
	if home, err := os.UserHomeDir(); err == nil {
		if err := readcache.Clear(readcache.DefaultDir(home)); err != nil {
			slog.Warn("read cache flush failed", "error", err)
		}
		for _, dir := range []string{
			toolusecache.DefaultDir(home),
			toolusecache.CollapsedKeysDir(home),
		} {
			if err := toolusecache.Clear(dir); err != nil {
				slog.Warn("tool-use cache flush failed", "dir", dir, "error", err)
			}
		}
	}
	slog.Info("all caches flushed")
}

// messageMentionsAnyPrunedTool flushes the per-session pruned-tools
// cache for any tool the current request's message text mentions. Used
// by T103b to decide which previously-pruned tool definitions to
// reattach. Returns the list of mentioned tool names (deduplicated).
func messageMentionsAnyPrunedTool(messages []types.Message, tracker *toolprune.UsageTracker, sessionID string) []string {
	if tracker == nil || sessionID == "" {
		return nil
	}
	candidates := tracker.PrunedToolNames(sessionID)
	if len(candidates) == 0 {
		return nil
	}
	var text strings.Builder
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "user" && role != "system" && role != "developer" {
			continue
		}
		for _, b := range msg.Content {
			if b.Text != "" {
				text.WriteString(b.Text + "\n")
			}
		}
	}
	return toolprune.MentionedTools(text.String(), candidates)
}

// extractUsedToolNames returns the distinct tool names from tool_use
// blocks in the message list. Used by T103 to feed the usage tracker.
func extractUsedToolNames(messages []types.Message) []string {
	return extractUsedToolNamesWithResolved(messages, nil)
}

func extractUsedToolNamesWithResolved(messages []types.Message, rememberedToolUses map[string]types.ContentBlock) []string {
	toolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), rememberedToolUses)
	return extractUsedToolNamesWithResolvedToolUses(messages, toolUses)
}

func extractUsedToolNamesWithResolvedToolUses(messages []types.Message, toolUses map[string]types.ContentBlock) []string {
	seen := make(map[string]bool)
	var names []string
	for _, msg := range messages {
		for _, block := range msg.Content {
			toolName := ""
			switch block.Type {
			case "tool_use":
				toolName = block.ToolName
			case "tool_result":
				if use, ok := proxyResolveToolUseDetailed(block, toolUses); ok {
					toolName = use.ToolName
				}
			}
			if toolName == "" || seen[toolName] {
				continue
			}
			seen[toolName] = true
			names = append(names, toolName)
		}
	}
	return names
}
