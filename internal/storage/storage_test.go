package storage

import (
	"os"
	"testing"
	"time"
)

func TestSaveAndLoadNote(t *testing.T) {
	dir := t.TempDir()
	s := NewDirStorage(dir)

	day := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	note, err := s.LoadNote(day)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if note.Content != "" {
		t.Error("expected empty content for non-existent file")
	}

	note.Content = "# March 15\n[] buy groceries\n[x] done thing"
	if err := s.SaveNote(note); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}

	// Reload
	loaded, err := s.LoadNote(day)
	if err != nil {
		t.Fatalf("LoadNote after save: %v", err)
	}
	if loaded.Content != note.Content {
		t.Errorf("content mismatch: got %q, want %q", loaded.Content, note.Content)
	}
	if len(loaded.Todos) != 2 {
		t.Errorf("expected 2 todos, got %d", len(loaded.Todos))
	}
}

func TestListNotes(t *testing.T) {
	dir := t.TempDir()
	s := NewDirStorage(dir)

	days := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	for _, d := range days {
		note, _ := s.LoadNote(d)
		note.Content = "# Note"
		if err := s.SaveNote(note); err != nil {
			t.Fatal(err)
		}
	}

	notes, err := s.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
	// Should be sorted newest first
	if !notes[0].Day.After(notes[1].Day) {
		t.Error("notes should be sorted newest first")
	}
}

func TestListNotes_SkipsNonDateFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewDirStorage(dir)

	// Create a non-date markdown file
	os.WriteFile(dir+"/readme.md", []byte("readme"), 0644)
	// Create a non-md file
	os.WriteFile(dir+"/2024-01-01.txt", []byte("txt"), 0644)

	notes, err := s.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes, got %d", len(notes))
	}
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir() + "/nested/path"
	s := NewDirStorage(dir)
	if err := s.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Error("expected directory to be created")
	}
}
