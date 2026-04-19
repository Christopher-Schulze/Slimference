package main

import "github.com/slimference/slimference/internal/tui"

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
