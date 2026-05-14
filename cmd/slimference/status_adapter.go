package main

import (
	"sort"

	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/tui"
)

func layer0StatusFromSnapshots(snaps map[string]filter.FilterSnapshot) tui.Layer0Status {
	status := tui.Layer0Status{Filters: make([]tui.Layer0FilterStatus, 0, len(snaps))}
	for name, snap := range snaps {
		row := tui.Layer0FilterStatus{
			Name:       name,
			Attempts:   snap.Attempts,
			Matches:    snap.Matches,
			Misses:     snap.Misses,
			Panics:     snap.Panics,
			BytesSaved: snap.BytesSaved,
			HitRate:    snap.HitRate,
			AvgMs:      snap.AvgMs,
		}
		status.Filters = append(status.Filters, row)
		status.Attempts += row.Attempts
		status.Matches += row.Matches
		status.Misses += row.Misses
		status.Panics += row.Panics
		status.BytesSaved += row.BytesSaved
	}
	if status.Attempts > 0 {
		status.HitRate = float64(status.Matches) / float64(status.Attempts)
	}
	sort.Slice(status.Filters, func(i, j int) bool {
		if status.Filters[i].BytesSaved != status.Filters[j].BytesSaved {
			return status.Filters[i].BytesSaved > status.Filters[j].BytesSaved
		}
		if status.Filters[i].Matches != status.Filters[j].Matches {
			return status.Filters[i].Matches > status.Filters[j].Matches
		}
		return status.Filters[i].Name < status.Filters[j].Name
	})
	return status
}

func (a *proxyAdapter) GetCheckpointStatus() tui.CheckpointStatus {
	status := a.p.AdminStatusSnapshot().Checkpoints
	return tui.CheckpointStatus{
		Count:       status.Count,
		Captures:    status.Captures,
		Restores:    status.Restores,
		Bytes:       status.Bytes,
		LastCapture: status.LastCapture,
		LastRestore: status.LastRestore,
		LastTrigger: status.LastTrigger,
	}
}

func (a *proxyAdapter) GetLayer0Status() tui.Layer0Status {
	return layer0StatusFromSnapshots(a.p.AdminStatusSnapshot().Layer0)
}

func (a *proxyAdapter) GetToolArchiveStatus() tui.ToolArchiveStatus {
	status := a.p.AdminStatusSnapshot().ToolArchive
	return tui.ToolArchiveStatus{
		Count:        status.Count,
		Archived:     status.Archived,
		Expanded:     status.Expanded,
		BytesRaw:     status.BytesRaw,
		BytesStored:  status.BytesStored,
		LastArchived: status.LastArchived,
		LastExpanded: status.LastExpanded,
	}
}
