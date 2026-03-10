package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"otto/internal/models"
	"otto/internal/parser"
)

// Storage defines the interface for note persistence.
type Storage interface {
	LoadNote(day time.Time) (*models.Note, error)
	SaveNote(note *models.Note) error
	ListNotes() ([]*models.Note, error)
	EnsureDir() error
}

// DirStorage stores notes as markdown files in a directory.
type DirStorage struct {
	Dir string
}

// NewDirStorage creates a DirStorage with the given directory.
// If dir is empty, it defaults to ~/.otto/notes/.
func NewDirStorage(dir string) *DirStorage {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".otto", "notes")
	}
	return &DirStorage{Dir: dir}
}

// EnsureDir creates the notes directory if it doesn't exist.
func (s *DirStorage) EnsureDir() error {
	return os.MkdirAll(s.Dir, 0755)
}

// filePath returns the file path for a given day.
func (s *DirStorage) filePath(day time.Time) string {
	return filepath.Join(s.Dir, fmt.Sprintf("%s.md", day.Format(models.DateFormat)))
}

// LoadNote loads the note for the given day. If the file doesn't exist, returns an empty note.
func (s *DirStorage) LoadNote(day time.Time) (*models.Note, error) {
	path := s.filePath(day)
	content := ""

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read note: %w", err)
	}
	if err == nil {
		content = string(data)
	}

	todos := parser.ParseTodos(content, day)
	return &models.Note{
		Day:      day,
		FilePath: path,
		Content:  content,
		Todos:    todos,
	}, nil
}

// SaveNote writes the note content to disk. The directory is created if needed.
func (s *DirStorage) SaveNote(note *models.Note) error {
	if err := s.EnsureDir(); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}
	path := s.filePath(note.Day)
	if err := os.WriteFile(path, []byte(note.Content), 0644); err != nil {
		return fmt.Errorf("write note: %w", err)
	}
	note.FilePath = path
	// Re-parse todos after save
	note.Todos = parser.ParseTodos(note.Content, note.Day)
	return nil
}

// ListNotes returns all notes sorted by day descending (newest first).
func (s *DirStorage) ListNotes() ([]*models.Note, error) {
	if err := s.EnsureDir(); err != nil {
		return nil, fmt.Errorf("ensure dir: %w", err)
	}

	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var notes []*models.Note
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		dateStr := strings.TrimSuffix(name, ".md")
		day, err := time.Parse(models.DateFormat, dateStr)
		if err != nil {
			continue // skip non-date files
		}
		note, err := s.LoadNote(day)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}

	sort.Slice(notes, func(i, j int) bool {
		return notes[i].Day.After(notes[j].Day)
	})

	return notes, nil
}
