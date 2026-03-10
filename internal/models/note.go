package models

import (
	"fmt"
	"time"
)

const DateFormat = "2006-01-02"

// Todo represents a single todo item parsed from a note file.
type Todo struct {
	ID       string    // "YYYY-MM-DD:<lineNum>"
	Day      time.Time
	Text     string
	Done     bool
	LineNum  int
	Priority int // 0=none, 1=highest…4=lowest
}

// IsOverdue returns true if the todo is not done and belongs to a past day.
func (t Todo) IsOverdue() bool {
	today := time.Now().Truncate(24 * time.Hour)
	day := t.Day.Truncate(24 * time.Hour)
	return !t.Done && day.Before(today)
}

// Note represents a single daily note file.
type Note struct {
	Day      time.Time
	FilePath string
	Content  string
	Todos    []Todo
}

// HasPendingTodos returns true if there is at least one incomplete todo.
func (n Note) HasPendingTodos() bool {
	for _, t := range n.Todos {
		if !t.Done {
			return true
		}
	}
	return false
}

// DateKey returns the date formatted as "YYYY-MM-DD".
func (n Note) DateKey() string {
	return fmt.Sprintf(n.Day.Format(DateFormat))
}
