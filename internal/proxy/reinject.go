package proxy

import (
	"os"
	"regexp"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// archiveURIPattern matches `local-archive://<id>` and the legacy
// `slim://archive/<id>` forms anywhere inside a content block. Used by
// T76 WP3 to re-inject archived original content when the model
// references it explicitly in a follow-up request.
var archiveURIPattern = regexp.MustCompile(`(?:local-archive://|slim://archive/)([A-Za-z0-9_\-]+)`)

// maxReinjectsPerRequest caps the number of archive expansions a single
// request can trigger. A runaway loop (e.g. archived content that
// itself references another archive id) cannot blow up the request
// budget. 8 is generous enough for hand-rolled debugging flows but
// keeps the worst case bounded.
const maxReinjectsPerRequest = 8

const defaultArchiveRecoveryNote = "If a tool result contains [context-archive ... uri=local-archive://<id>] or [context-chunk ... uri=local-archive://<id>], request that exact URI only when the full elided content is needed."

func archiveRecoveryNoteText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultArchiveRecoveryNote
	}
	return text
}

func (p *Proxy) reserveArchiveRecoveryNote(sessionID string, enabled bool) bool {
	if p == nil || p.config == nil || !enabled {
		return false
	}
	sessionID = sessions.SafeOptionalSessionID(sessionID)
	if sessionID == "" {
		return false
	}
	p.archiveRecoveryNoteMu.Lock()
	defer p.archiveRecoveryNoteMu.Unlock()
	if p.archiveRecoveryNote == nil {
		p.archiveRecoveryNote = make(map[string]struct{})
	}
	if _, seen := p.archiveRecoveryNote[sessionID]; seen {
		return false
	}
	p.archiveRecoveryNote[sessionID] = struct{}{}
	return true
}

func (p *Proxy) forgetArchiveRecoveryNote(sessionID string) {
	if p == nil {
		return
	}
	sessionID = sessions.SafeOptionalSessionID(sessionID)
	if sessionID == "" {
		return
	}
	p.archiveRecoveryNoteMu.Lock()
	defer p.archiveRecoveryNoteMu.Unlock()
	delete(p.archiveRecoveryNote, sessionID)
}

// reinjectArchivedContent scans message text for local-archive URIs and
// appends expansions as additional content blocks on the same message.
// Counters are bumped per successful expansion so
// /admin/status.content_archive.re_inject_count reflects the activity.
func (p *Proxy) reinjectArchivedContent(messages []types.Message) []types.Message {
	return p.reinjectArchivedContentForSession("", messages)
}

func (p *Proxy) reinjectArchivedContentForSession(sessionID string, messages []types.Message) []types.Message {
	if len(messages) == 0 {
		return messages
	}
	sessionID = sessions.SafeOptionalSessionID(sessionID)
	home, err := os.UserHomeDir()
	if err != nil {
		return messages
	}
	archiveDir := contentarchive.DefaultDir(home)
	expansionsLeft := maxReinjectsPerRequest

	for i := range messages {
		if expansionsLeft <= 0 {
			break
		}
		seen := map[string]struct{}{}
		var expansions []types.ContentBlock
		for j := range messages[i].Content {
			text := messages[i].Content[j].Text
			if text == "" {
				continue
			}
			matches := archiveURIPattern.FindAllStringSubmatch(text, -1)
			for _, m := range matches {
				if expansionsLeft <= 0 {
					break
				}
				id := m[1]
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				entry, _, err := contentarchive.Peek(archiveDir, id)
				if err != nil {
					continue
				}
				if !archiveEntryMatchesSession(entry, sessionID) {
					continue
				}
				_, body, err := contentarchive.Get(archiveDir, id)
				if err != nil {
					continue
				}
				expansions = append(expansions, types.ContentBlock{
					Type: "text",
					Text: "[reinjected from " + m[0] + "]\n" + string(body),
				})
				contentarchive.RecordReInjectBytes(archiveDir, len(body))
				if p.qualityNetSavings != nil {
					p.qualityNetSavings.RecordInvalidation(len(body) / 4)
				}
				expansionsLeft--
			}
		}
		if len(expansions) > 0 {
			messages[i].Content = append(messages[i].Content, expansions...)
		}
	}
	return messages
}

func archiveEntryMatchesSession(entry *contentarchive.Entry, sessionID string) bool {
	if entry == nil {
		return false
	}
	archiveSessionID := sessions.SafeOptionalSessionID(entry.SessionID)
	if archiveSessionID == "" || sessionID == "" {
		return true
	}
	return archiveSessionID == sessionID
}

// hasArchiveReference reports whether s contains any local-archive URI.
// Exposed so tests and other call sites can probe without rebuilding
// the regex. T76 WP3.
func hasArchiveReference(s string) bool {
	return archiveURIPattern.MatchString(s)
}

// extractArchiveIDs returns every distinct archive id mentioned in s,
// in order of first appearance. Used by T103 (tool definition pruning)
// to learn which archived tool schemas should be reattached.
func extractArchiveIDs(s string) []string {
	if s == "" {
		return nil
	}
	matches := archiveURIPattern.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		// Regex captures `[A-Za-z0-9_\-]+` so trimming the match group
		// is only a defensive normalisation; whitespace cannot appear
		// in the captured slice.
		id := strings.TrimSpace(m[1])
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
