package main

import "github.com/Christopher-Schulze/Slimference/internal/tui"

func (p *testTUIProxy) GetCheckpointStatus() tui.CheckpointStatus {
	return tui.CheckpointStatus{}
}

func (p *testTUIProxy) GetToolArchiveStatus() tui.ToolArchiveStatus {
	return tui.ToolArchiveStatus{}
}
