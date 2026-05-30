package readcache

import (
	"os"
	"path/filepath"
	"strings"
)

const statsFilename = "stats.json"

func LoadStats(dir string) (Stats, error) {
	data, err := readCacheReadFile(filepath.Join(dir, statsFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return Stats{}, nil
		}
		return Stats{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return Stats{}, nil
	}
	var stats Stats
	if err := readCacheUnmarshal(data, &stats); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func SaveStats(dir string, stats Stats) error {
	if err := readCacheMkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := readCacheMarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	return readCacheWriteFile(filepath.Join(dir, statsFilename), append(data, '\n'), 0o644)
}

func Snapshot(dir string) (Stats, error) {
	stats, err := LoadStats(dir)
	if err != nil {
		return Stats{}, err
	}
	entries, err := readCacheReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return Stats{}, err
	}
	stats.Sessions = 0
	stats.TrackedFiles = 0
	stats.TrackedOutputs = 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == statsFilename || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		state, err := LoadSession(dir, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return Stats{}, err
		}
		stats.Sessions++
		stats.TrackedFiles += len(state.Files)
		stats.TrackedOutputs += len(state.Outputs)
	}
	return stats, nil
}

func RecordDecision(dir string, decision Decision) error {
	stats, err := LoadStats(dir)
	if err != nil {
		return err
	}
	stats.Evaluations++
	if decision.Type == DecisionBlock {
		stats.Blocks++
		switch decision.BlockKind {
		case BlockKindUnchanged:
			stats.UnchangedBlocks++
		case BlockKindDelta:
			stats.DeltaBlocks++
		}
	} else {
		stats.Allows++
	}
	return SaveStats(dir, stats)
}

func Clear(dir string) error {
	clearMemoryDir(dir)
	if err := readCacheRemoveAll(dir); err != nil {
		return err
	}
	return nil
}
