package todopane

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"otto/internal/models"
	"otto/internal/tui/styles"
)

// TodoTextEditedMsg is sent when the user finishes inline-editing a todo's text.
type TodoTextEditedMsg struct {
	Day     time.Time
	LineNum int
	NewText string
}

// Model is the todo pane that displays aggregated todos.
type Model struct {
	viewport      viewport.Model
	editInput     textinput.Model
	searchInput   textinput.Model
	todos         []models.Todo
	filteredTodos []models.Todo
	selectedIdx   int
	editing       bool
	searching     bool
	active        bool
	hovered       bool
	width         int
	height        int
	ready         bool
}

// New creates a new todo pane.
func New() Model {
	edit := textinput.New()
	edit.Prompt = ""
	edit.CharLimit = 0

	search := textinput.New()
	search.Prompt = ""
	search.Placeholder = "search…"
	search.CharLimit = 40

	return Model{editInput: edit, searchInput: search}
}

// SetTodos replaces the displayed todos and re-applies the current filter.
func (m *Model) SetTodos(todos []models.Todo) {
	m.todos = todos
	m.refilter()
}

// refilter applies the current search query and updates the viewport.
func (m *Model) refilter() {
	q := strings.ToLower(m.searchInput.Value())
	if q == "" {
		m.filteredTodos = m.todos
	} else {
		m.filteredTodos = nil
		for _, t := range m.todos {
			if strings.Contains(strings.ToLower(t.Text), q) ||
				strings.Contains(strings.ToLower(t.Day.Format(models.DateFormat)), q) {
				m.filteredTodos = append(m.filteredTodos, t)
			}
		}
	}
	// Clamp selectedIdx to filtered range.
	if len(m.filteredTodos) == 0 {
		m.selectedIdx = 0
	} else if m.selectedIdx >= len(m.filteredTodos) {
		m.selectedIdx = len(m.filteredTodos) - 1
	}
	m.viewport.SetContent(m.renderContent())
}

// SetSize sets the pane dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	innerW := w - 4
	// Reserve 2 rows for the search bar inside the border.
	innerH := h - 6
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	if !m.ready {
		m.viewport = viewport.New(innerW, innerH)
		m.ready = true
	} else {
		m.viewport.Width = innerW
		m.viewport.Height = innerH
	}
	m.searchInput.Width = w - 6
	m.editInput.Width = w - 8
	m.viewport.SetContent(m.renderContent())
}

// SetActive sets focus state.
func (m *Model) SetActive(active bool) {
	m.active = active
	if !active {
		m.editInput.Blur()
		m.searchInput.Blur()
		return
	}
	// When becoming active, restore focus based on current sub-mode.
	if m.editing {
		_ = m.editInput.Focus()
	} else if m.searching {
		_ = m.searchInput.Focus()
	}
	// Otherwise navigation mode: no input focused, j/k works immediately.
}

// SetHovered sets the hovered state (pane navigation cursor).
func (m *Model) SetHovered(hovered bool) {
	m.hovered = hovered
}

// SelectedTodo returns a pointer to the currently selected filtered todo, or nil if empty.
func (m *Model) SelectedTodo() *models.Todo {
	if len(m.filteredTodos) == 0 || m.selectedIdx >= len(m.filteredTodos) {
		return nil
	}
	return &m.filteredTodos[m.selectedIdx]
}

// IsEditing returns true when the pane is in inline edit mode.
func (m Model) IsEditing() bool {
	return m.editing
}

// renderContent builds the todo list text.
func (m Model) renderContent() string {
	if len(m.filteredTodos) == 0 {
		return styles.Dim.Render("No todos yet.")
	}

	var sb strings.Builder
	for i, t := range m.filteredTodos {
		var line string
		if i == m.selectedIdx && m.editing {
			line = "  " + m.editInput.View()
		} else {
			line = formatTodo(t)
			if i == m.selectedIdx {
				line = styles.TodoSelected.Render("> ") + line
			} else {
				line = "  " + line
			}
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatTodo(t models.Todo) string {
	dateStr := t.Day.Format(models.DateFormat)

	switch {
	case t.IsOverdue():
		prefix := styles.TodoOverdue.Render("[!]")
		text := styles.TodoOverdue.Render(fmt.Sprintf(" %s  %s", dateStr, t.Text))
		return prefix + text
	case t.Done:
		line := fmt.Sprintf("[x] %s  %s", dateStr, t.Text)
		return styles.TodoDone.Render(line)
	case t.Priority > 0:
		s := styles.PriorityStyle(t.Priority)
		prefix := s.Render("[!]")
		text := s.Render(fmt.Sprintf(" %s  %s", dateStr, t.Text))
		return prefix + text
	default:
		prefix := "[ ]"
		text := fmt.Sprintf(" %s  %s", dateStr, t.Text)
		return prefix + styles.TodoPending.Render(text)
	}
}

// Update handles keyboard events.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// ── Inline edit mode ─────────────────────────────────────────────────
		if m.editing {
			switch keyMsg.String() {
			case "enter":
				text := strings.TrimSpace(m.editInput.Value())
				todo := m.filteredTodos[m.selectedIdx]
				m.editing = false
				m.editInput.Blur()
				m.viewport.SetContent(m.renderContent())
				if text != "" && text != todo.Text {
					return m, func() tea.Msg {
						return TodoTextEditedMsg{
							Day:     todo.Day,
							LineNum: todo.LineNum,
							NewText: text,
						}
					}
				}
				return m, nil
			case "esc":
				m.editing = false
				m.editInput.Blur()
				m.viewport.SetContent(m.renderContent())
				return m, nil
			default:
				m.editInput, cmd = m.editInput.Update(keyMsg)
				m.viewport.SetContent(m.renderContent())
				return m, cmd
			}
		}

		// ── Search mode ───────────────────────────────────────────────────────
		if m.searching {
			switch keyMsg.String() {
			case "esc", "enter":
				m.searching = false
				m.searchInput.Blur()
				if keyMsg.String() == "esc" {
					m.searchInput.SetValue("")
					m.refilter()
				}
				return m, nil
			default:
				m.searchInput, cmd = m.searchInput.Update(keyMsg)
				m.refilter()
				return m, cmd
			}
		}

		// ── Navigation mode ───────────────────────────────────────────────────
		switch keyMsg.String() {
		case "j", "down":
			if m.selectedIdx < len(m.filteredTodos)-1 {
				m.selectedIdx++
				m.viewport.SetContent(m.renderContent())
				if m.selectedIdx >= m.viewport.YOffset+m.viewport.Height {
					m.viewport.YOffset = m.selectedIdx - m.viewport.Height + 1
				}
			}
			return m, nil
		case "k", "up":
			if m.selectedIdx > 0 {
				m.selectedIdx--
				m.viewport.SetContent(m.renderContent())
				if m.selectedIdx < m.viewport.YOffset {
					m.viewport.YOffset = m.selectedIdx
				}
			}
			return m, nil
		case "ctrl+d":
			m.viewport.HalfViewDown()
			return m, nil
		case "ctrl+u":
			m.viewport.HalfViewUp()
			return m, nil
		case "g":
			m.selectedIdx = 0
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoTop()
			return m, nil
		case "G":
			if len(m.filteredTodos) > 0 {
				m.selectedIdx = len(m.filteredTodos) - 1
			}
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoBottom()
			return m, nil
		case "i":
			if len(m.filteredTodos) > 0 && m.selectedIdx < len(m.filteredTodos) {
				todo := m.filteredTodos[m.selectedIdx]
				m.editInput.SetValue(todo.Text)
				m.editInput.CursorEnd()
				cmd = m.editInput.Focus()
				m.editing = true
				m.viewport.SetContent(m.renderContent())
			}
			return m, cmd
		case "/":
			m.searching = true
			cmd = m.searchInput.Focus()
			return m, cmd
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View renders the todo pane.
func (m Model) View() string {
	border := styles.BorderState(m.active, m.hovered)
	title := styles.Title.Render("Todos")

	var content string
	if m.ready {
		content = m.viewport.View()
	}

	searchBar := styles.Dim.Render("/ ") + m.searchInput.View()
	inner := lipgloss.JoinVertical(lipgloss.Left,
		title,
		content,
		searchBar,
	)
	return border.Width(m.width - 2).Height(m.height - 2).Render(inner)
}
