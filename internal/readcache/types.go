package readcache

type Request struct {
	SessionID               string
	TurnID                  string
	FilePath                string
	Offset                  int
	Limit                   int
	RecentFullPassTurnLimit int
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

type OutputRequest struct {
	SessionID   string
	TurnID      string
	Key         string
	CommandLine string
}

type FileEntry struct {
	Path          string `json:"path"`
	LastTurnID    string `json:"last_turn_id,omitempty"`
	LastTurnSeq   int    `json:"last_turn_seq,omitempty"`
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
	ModTimeUnixNs int64  `json:"mod_time_unix_ns"`
	ContentHash   string `json:"content_hash,omitempty"`
	ArchiveURI    string `json:"archive_uri,omitempty"`
	CachedContent string `json:"cached_content,omitempty"`
}

type OutputEntry struct {
	Key           string `json:"key"`
	CommandLine   string `json:"command_line,omitempty"`
	LastTurnID    string `json:"last_turn_id,omitempty"`
	LastTurnSeq   int    `json:"last_turn_seq,omitempty"`
	ContentHash   string `json:"content_hash,omitempty"`
	ArchiveURI    string `json:"archive_uri,omitempty"`
	CachedContent string `json:"cached_content,omitempty"`
}

type SessionState struct {
	SessionID     string                  `json:"session_id"`
	CurrentTurnID string                  `json:"current_turn_id,omitempty"`
	TurnSeq       int                     `json:"turn_seq,omitempty"`
	Files         map[string]*FileEntry   `json:"files"`
	Outputs       map[string]*OutputEntry `json:"outputs,omitempty"`
}

type Stats struct {
	Evaluations     int `json:"evaluations"`
	Allows          int `json:"allows"`
	Blocks          int `json:"blocks"`
	UnchangedBlocks int `json:"unchanged_blocks"`
	DeltaBlocks     int `json:"delta_blocks"`
	Sessions        int `json:"sessions"`
	TrackedFiles    int `json:"tracked_files"`
	TrackedOutputs  int `json:"tracked_outputs"`
}
