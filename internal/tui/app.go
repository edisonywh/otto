package tui

import (
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"otto/internal/models"
	"otto/internal/parser"
	"otto/internal/storage"
	"otto/internal/tui/editor"
	"otto/internal/tui/filebrowser"
	"otto/internal/tui/todopane"
)

// FocusTarget identifies which pane is focused.
type FocusTarget int

const (
	FocusEditor FocusTarget = iota
	FocusTodoPane
	FocusFileBrowser
)

// AppNavMode represents the app-level navigation state.
type AppNavMode int

const (
	AppNavModeActive AppNavMode = iota // inside a pane (blue border)
	AppNavModePane                     // hovering between panes (orange border)
)

// AppModel is the root BubbleTea model.
type AppModel struct {
	fileBrowser filebrowser.Model
	todoPane    todopane.Model
	editor      editor.Model
	focus       FocusTarget
	navMode     AppNavMode
	notes       []*models.Note
	activeNote  *models.Note
	storage     storage.Storage
	width       int
	height      int
	ready       bool
	err         error
}

// New creates the root model.
func New(s storage.Storage) AppModel {
	fb := filebrowser.New()
	tp := todopane.New()
	ed := editor.New()

	m := AppModel{
		fileBrowser: fb,
		todoPane:    tp,
		editor:      ed,
		focus:       FocusEditor,
		navMode:     AppNavModeActive,
		storage:     s,
	}
	m.editor.SetActive(true)
	return m
}

// Init starts the app by loading all notes.
func (m AppModel) Init() tea.Cmd {
	return loadAllNotesCmd(m.storage)
}

// loadAllNotesCmd loads all notes from storage, ensuring today's note exists.
func loadAllNotesCmd(s storage.Storage) tea.Cmd {
	return func() tea.Msg {
		notes, err := s.ListNotes()
		if err != nil {
			return ErrMsg{Err: err}
		}

		today := time.Now().Truncate(24 * time.Hour)
		found := false
		for _, n := range notes {
			if n.Day.Equal(today) {
				found = true
				break
			}
		}
		if !found {
			note, err := s.LoadNote(today)
			if err != nil {
				return ErrMsg{Err: err}
			}
			notes = append([]*models.Note{note}, notes...)
		}

		return NotesLoadedMsg{Notes: notes}
	}
}

// clearSavedCmd fires after a short delay to hide the "Saved" indicator.
func clearSavedCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return ClearSavedMsg{}
	})
}

// saveNoteCmd persists a note to disk.
func saveNoteCmd(s storage.Storage, note *models.Note) tea.Cmd {
	return func() tea.Msg {
		if note == nil {
			return nil
		}
		if err := s.SaveNote(note); err != nil {
			return ErrMsg{Err: err}
		}
		return NoteSavedMsg{Note: note}
	}
}

// refreshTodosCmd collects todos from all in-memory notes.
func refreshTodosCmd(notes []*models.Note) tea.Cmd {
	return func() tea.Msg {
		todos := parser.CollectAllTodos(notes)
		return TodosRefreshedMsg{Todos: todos}
	}
}

// Update is the main BubbleTea update function.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.distributeSize()
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Sequence(saveNoteCmd(m.storage, m.activeNote), tea.Quit)
		case "ctrl+s":
			return m, saveNoteCmd(m.storage, m.activeNote)
		case "esc":
			if m.focus == FocusTodoPane && m.todoPane.IsEditing() {
				// Let TodoPane handle Esc (cancels inline edit)
				break
			}
			if m.focus == FocusEditor {
				if m.editor.Mode() == editor.ModeInsert {
					// Fall through: editor handles Esc → Normal mode
					break
				}
				// Normal or Preview → NavPane
				m.navMode = AppNavModePane
				m.setFocus(m.focus)
				return m, nil
			}
			// FileBrowser or TodoPane → NavPane immediately
			m.navMode = AppNavModePane
			m.setFocus(m.focus)
			return m, nil
		case "h", "j", "k", "l":
			if m.navMode == AppNavModePane {
				wasEditor := m.focus == FocusEditor
				switch msg.String() {
				case "h":
					m.setFocus(FocusFileBrowser)
				case "l":
					if m.focus == FocusFileBrowser {
						m.setFocus(FocusEditor)
					}
				case "j":
					if m.focus == FocusFileBrowser || m.focus == FocusTodoPane {
						m.setFocus(FocusEditor)
					}
				case "k":
					if m.focus == FocusEditor {
						m.setFocus(FocusTodoPane)
					}
				}
				if wasEditor && m.focus != FocusEditor {
					return m, saveNoteCmd(m.storage, m.activeNote)
				}
				return m, nil
			}
			// NavActive: fall through to updateFocused
		case "1", "2", "3", "4":
			if m.navMode == AppNavModeActive && m.focus == FocusTodoPane && !m.todoPane.IsEditing() {
				priority, _ := strconv.Atoi(msg.String())
				todo := m.todoPane.SelectedTodo()
				if todo != nil {
					for _, note := range m.notes {
						if note.Day.Equal(todo.Day) {
							newContent := parser.SetPriority(note.Content, todo.LineNum, priority)
							note.Content = newContent
							note.Todos = parser.ParseTodos(newContent, note.Day)
							if m.activeNote != nil && m.activeNote.Day.Equal(note.Day) {
								m.editor.SetNote(note)
							}
							return m, tea.Batch(saveNoteCmd(m.storage, note), refreshTodosCmd(m.notes))
						}
					}
				}
				return m, nil
			}
			// NavPane or other panes: fall through to updateFocused
		case "tab":
			if m.focus == FocusEditor && m.editor.Mode() == editor.ModeInsert {
				break // let textarea handle tab (insert character)
			}
			m.navMode = AppNavModeActive
			return m, m.cycleFocus(1)
		case "shift+tab":
			m.navMode = AppNavModeActive
			return m, m.cycleFocus(-1)
		case "enter":
			if m.navMode == AppNavModePane {
				m.navMode = AppNavModeActive
				m.setFocus(m.focus)
				if m.focus == FocusEditor {
					m.editor.EnterInsert()
				}
				return m, nil
			}
			if m.focus == FocusTodoPane && !m.todoPane.IsEditing() {
				todo := m.todoPane.SelectedTodo()
				if todo != nil {
					for _, note := range m.notes {
						if note.Day.Equal(todo.Day) {
							newContent := parser.ToggleLine(note.Content, todo.LineNum)
							note.Content = newContent
							note.Todos = parser.ParseTodos(newContent, note.Day)
							if m.activeNote != nil && m.activeNote.Day.Equal(note.Day) {
								m.editor.SetNote(note)
							}
							return m, tea.Batch(saveNoteCmd(m.storage, note), refreshTodosCmd(m.notes))
						}
					}
				}
				return m, nil
			}
			if m.focus == FocusFileBrowser {
				selected := m.fileBrowser.SelectedNote()
				if selected != nil && (m.activeNote == nil || !selected.Day.Equal(m.activeNote.Day)) {
					saveCmd := saveNoteCmd(m.storage, m.activeNote)
					m.activeNote = selected
					m.editor.SetNote(selected)
					return m, tea.Sequence(saveCmd, refreshTodosCmd(m.notes))
				}
				return m, nil
			}
		}

	case NotesLoadedMsg:
		m.notes = msg.Notes
		m.fileBrowser.SetNotes(m.notes)
		if len(m.notes) > 0 {
			m.activeNote = m.notes[0]
			m.editor.SetNote(m.activeNote)
		}
		return m, refreshTodosCmd(m.notes)

	case NoteSavedMsg:
		for i, n := range m.notes {
			if n.Day.Equal(msg.Note.Day) {
				m.notes[i] = msg.Note
				break
			}
		}
		m.fileBrowser.SetNotes(m.notes)
		m.editor.SetSaved(true)
		return m, tea.Batch(refreshTodosCmd(m.notes), clearSavedCmd())

	case ClearSavedMsg:
		m.editor.SetSaved(false)
		return m, nil

	case TodosRefreshedMsg:
		m.todoPane.SetTodos(msg.Todos)
		return m, nil

	case todopane.TodoTextEditedMsg:
		for _, note := range m.notes {
			if note.Day.Equal(msg.Day) {
				newContent := parser.SetTodoText(note.Content, msg.LineNum, msg.NewText)
				note.Content = newContent
				note.Todos = parser.ParseTodos(newContent, note.Day)
				if m.activeNote != nil && m.activeNote.Day.Equal(note.Day) {
					m.editor.SetNote(note)
				}
				return m, tea.Batch(saveNoteCmd(m.storage, note), refreshTodosCmd(m.notes))
			}
		}
		return m, nil

	case ErrMsg:
		m.err = msg.Err
		return m, nil
	}

	return m.updateFocused(msg)
}

// updateFocused forwards messages to the focused pane.
func (m AppModel) updateFocused(msg tea.Msg) (tea.Model, tea.Cmd) {
	// In pane navigation mode, swallow all key events — only hjkl/Enter/Esc/Tab
	// are handled above; nothing else should mutate pane content.
	if _, isKey := msg.(tea.KeyMsg); isKey && m.navMode == AppNavModePane {
		return m, nil
	}

	var cmd tea.Cmd

	switch m.focus {
	case FocusEditor:
		var oldContent string
		if m.activeNote != nil {
			oldContent = m.activeNote.Content
		}
		m.editor, cmd = m.editor.Update(msg)
		if m.activeNote != nil {
			newContent := m.editor.Content()
			if newContent != oldContent {
				m.activeNote.Content = newContent
				m.activeNote.Todos = parser.ParseTodos(newContent, m.activeNote.Day)
				return m, tea.Batch(cmd, refreshTodosCmd(m.notes))
			}
		}

	case FocusTodoPane:
		m.todoPane, cmd = m.todoPane.Update(msg)

	case FocusFileBrowser:
		m.fileBrowser, cmd = m.fileBrowser.Update(msg)
	}

	return m, cmd
}

// cycleFocus advances focus in the given direction (1=forward, -1=backward).
func (m *AppModel) cycleFocus(dir int) tea.Cmd {
	var saveCmd tea.Cmd
	if m.focus == FocusEditor {
		saveCmd = saveNoteCmd(m.storage, m.activeNote)
	}

	order := []FocusTarget{FocusEditor, FocusTodoPane, FocusFileBrowser}
	current := int(m.focus)
	next := (current + dir + len(order)) % len(order)
	m.setFocus(FocusTarget(next))

	return saveCmd
}

func (m *AppModel) setFocus(f FocusTarget) {
	m.focus = f
	isActive := m.navMode == AppNavModeActive

	m.editor.SetActive(isActive && f == FocusEditor)
	m.editor.SetHovered(!isActive && f == FocusEditor)

	m.todoPane.SetActive(isActive && f == FocusTodoPane)
	m.todoPane.SetHovered(!isActive && f == FocusTodoPane)

	m.fileBrowser.SetActive(isActive && f == FocusFileBrowser)
	m.fileBrowser.SetHovered(!isActive && f == FocusFileBrowser)
}

// distributeSize assigns dimensions to each pane.
func (m *AppModel) distributeSize() {
	leftW := m.width * 27 / 100
	rightW := m.width - leftW
	topH := m.height * 25 / 100
	bottomH := m.height - topH

	m.fileBrowser.SetSize(leftW, m.height)
	m.todoPane.SetSize(rightW, topH)
	m.editor.SetSize(rightW, bottomH)
}

// View renders the full layout.
func (m AppModel) View() string {
	if !m.ready {
		return "Loading..."
	}
	if m.err != nil {
		return "Error: " + m.err.Error()
	}

	right := lipgloss.JoinVertical(lipgloss.Left, m.todoPane.View(), m.editor.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, m.fileBrowser.View(), right)
}
