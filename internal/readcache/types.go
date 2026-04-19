package readcache

type Request struct {
	SessionID string
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
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
	ModTimeUnixNs int64  `json:"mod_time_unix_ns"`
	CachedContent string `json:"cached_content,omitempty"`
}

type SessionState struct {
	SessionID string                `json:"session_id"`
	Files     map[string]*FileEntry `json:"files"`
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
