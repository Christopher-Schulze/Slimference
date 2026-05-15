package readcache

type Request struct {
	SessionID string
	TurnID    string
	FilePath  string
	Offset    int
	Limit     int
}

func (r Request) IsFullFileRead() bool {
	return r.Offset == 0 && r.Limit == 0
}

type DecisionType string

const (
	DecisionAllow DecisionType = "allow"
	DecisionBlock DecisionType = "block"
)

type BlockKind string

const (
	BlockKindNone      BlockKind = ""
	BlockKindUnchanged BlockKind = "unchanged"
	BlockKindDelta     BlockKind = "delta"
)

type Decision struct {
	Type      DecisionType
	Reason    string
	BlockKind BlockKind
}

type FileEntry struct {
	Path          string `json:"path"`
	LastTurnID    string `json:"last_turn_id,omitempty"`
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
	ModTimeUnixNs int64  `json:"mod_time_unix_ns"`
	ContentHash   string `json:"content_hash,omitempty"`
	ArchiveURI    string `json:"archive_uri,omitempty"`
	CachedContent string `json:"cached_content,omitempty"`
}

type SessionState struct {
	SessionID     string                `json:"session_id"`
	CurrentTurnID string                `json:"current_turn_id,omitempty"`
	Files         map[string]*FileEntry `json:"files"`
}

type Stats struct {
	Evaluations     int `json:"evaluations"`
	Allows          int `json:"allows"`
	Blocks          int `json:"blocks"`
	UnchangedBlocks int `json:"unchanged_blocks"`
	DeltaBlocks     int `json:"delta_blocks"`
	Sessions        int `json:"sessions"`
	TrackedFiles    int `json:"tracked_files"`
}
