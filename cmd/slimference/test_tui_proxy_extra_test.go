package main

import "github.com/slimference/slimference/internal/tui"

func (p *testTUIProxy) GetCheckpointStatus() tui.CheckpointStatus {
	return tui.CheckpointStatus{}
}

func (p *testTUIProxy) GetToolArchiveStatus() tui.ToolArchiveStatus {
	return tui.ToolArchiveStatus{}
}
