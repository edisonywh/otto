package tui

import "otto/internal/models"

// NotesLoadedMsg is sent when all notes have been loaded from disk.
type NotesLoadedMsg struct {
	Notes []*models.Note
}

// NoteUpdatedMsg is sent when a note's content has changed (in-memory or saved).
type NoteUpdatedMsg struct {
	Note *models.Note
}

// NoteSavedMsg is sent after a note has been successfully persisted to disk.
type NoteSavedMsg struct {
	Note *models.Note
}

// TodosRefreshedMsg is sent when the aggregated todo list has been recalculated.
type TodosRefreshedMsg struct {
	Todos []models.Todo
}

// ErrMsg carries an error to be displayed.
type ErrMsg struct {
	Err error
}

// ClearSavedMsg is sent after the "Saved" indicator timeout expires.
type ClearSavedMsg struct{}
