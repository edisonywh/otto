package filebrowser

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"otto/internal/models"
	"otto/internal/tui/styles"
)

// item wraps a Note for the bubbles list.
type item struct {
	note *models.Note
}

func (i item) Title() string       { return i.note.DateKey() }
func (i item) Description() string { return "" }
func (i item) FilterValue() string { return i.note.DateKey() }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

var selectedItemStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder(), false, false, false, true).
	BorderForeground(lipgloss.Color("99")).
	Foreground(lipgloss.Color("207")).
	PaddingLeft(1)

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}
	icon := " "
	if i.note.HasPendingTodos() {
		icon = "●"
	}
	dateStr := icon + " " + i.note.DateKey()
	if index == m.Index() {
		fmt.Fprint(w, selectedItemStyle.Render(dateStr))
	} else {
		var styledIcon string
		if i.note.HasPendingTodos() {
			styledIcon = styles.PendingIconStyle.Render("●")
		} else {
			styledIcon = " "
		}
		fmt.Fprint(w, "  "+styledIcon+" "+i.note.DateKey())
	}
}

// Model is the file browser pane.
type Model struct {
	list        list.Model
	searchInput textinput.Model
	notes       []*models.Note
	filtered    []*models.Note
	active      bool
	hovered     bool
	width       int
	height      int
}

// New creates a new file browser model.
func New() Model {
	l := list.New(nil, itemDelegate{}, 0, 0)
	l.Title = "Notes"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69")).Padding(0, 1)

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "filter…"
	ti.CharLimit = 20

	return Model{list: l, searchInput: ti}
}

// SetNotes updates the list with the given notes.
func (m *Model) SetNotes(notes []*models.Note) {
	m.notes = notes
	m.refilter()
}

// refilter applies the current search query to m.notes and updates the list.
func (m *Model) refilter() {
	q := strings.ToLower(m.searchInput.Value())
	if q == "" {
		m.filtered = m.notes
	} else {
		m.filtered = nil
		for _, n := range m.notes {
			if strings.Contains(strings.ToLower(n.DateKey()), q) {
				m.filtered = append(m.filtered, n)
			}
		}
	}
	items := make([]list.Item, len(m.filtered))
	for i, n := range m.filtered {
		items[i] = item{note: n}
	}
	m.list.SetItems(items)
}

// SetSize sets the pane dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Reserve 2 rows for the search bar (1 line + 1 spacing) inside the border.
	m.list.SetSize(w-2, h-4)
	m.searchInput.Width = w - 6
}

// SetActive sets focus state.
func (m *Model) SetActive(active bool) {
	m.active = active
	if active {
		_ = m.searchInput.Focus()
	} else {
		m.searchInput.Blur()
	}
}

// SetHovered sets the hovered state (pane navigation cursor).
func (m *Model) SetHovered(hovered bool) {
	m.hovered = hovered
}

// SelectDate selects the note with the given date key in the list.
func (m *Model) SelectDate(dateKey string) {
	for i, n := range m.filtered {
		if n.DateKey() == dateKey {
			m.list.Select(i)
			return
		}
	}
}

// SelectedNote returns the currently highlighted note.
func (m Model) SelectedNote() *models.Note {
	sel := m.list.SelectedItem()
	if sel == nil {
		return nil
	}
	return sel.(item).note
}

// Update handles keyboard events.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "j", "down":
			var listMsg tea.Msg = tea.KeyMsg{Type: tea.KeyDown}
			m.list, cmd = m.list.Update(listMsg)
			return m, cmd
		case "k", "up":
			var listMsg tea.Msg = tea.KeyMsg{Type: tea.KeyUp}
			m.list, cmd = m.list.Update(listMsg)
			return m, cmd
		case "g":
			m.list.Select(0)
			return m, nil
		case "G":
			if len(m.filtered) > 0 {
				m.list.Select(len(m.filtered) - 1)
			}
			return m, nil
		default:
			// All other keys go to the search input.
			m.searchInput, cmd = m.searchInput.Update(keyMsg)
			m.refilter()
			return m, cmd
		}
	}
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the file browser.
func (m Model) View() string {
	border := styles.BorderState(m.active, m.hovered)

	searchBar := styles.Dim.Render("/ ") + m.searchInput.View()
	inner := lipgloss.JoinVertical(lipgloss.Left, m.list.View(), searchBar)
	return border.Width(m.width - 2).Height(m.height - 2).Render(inner)
}
