package tui

func (m *mockProxy) GetCheckpointStatus() CheckpointStatus {
	return CheckpointStatus{}
}

func (m *mockProxy) GetToolArchiveStatus() ToolArchiveStatus {
	return ToolArchiveStatus{}
}
