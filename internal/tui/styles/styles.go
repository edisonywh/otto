package styles

import "github.com/charmbracelet/lipgloss"

var (
	colorBorderNormal  = lipgloss.Color("240")
	colorBorderActive  = lipgloss.Color("69")
	colorBorderHovered = lipgloss.Color("214")
	colorOverdue       = lipgloss.Color("196")
	colorDone          = lipgloss.Color("240")
	colorPending       = lipgloss.Color("255")
	colorPendingIcon   = lipgloss.Color("214")

	borderNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderNormal)

	borderActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderActive)

	borderHovered = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderHovered)

	TodoOverdue = lipgloss.NewStyle().
			Foreground(colorOverdue).
			Bold(true)

	TodoDone = lipgloss.NewStyle().
			Foreground(colorDone).
			Strikethrough(true)

	TodoPending = lipgloss.NewStyle().
			Foreground(colorPending)

	TodoSelected = lipgloss.NewStyle().
			Foreground(colorBorderHovered).
			Bold(true)

	TodoPriority1 = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).Bold(true)

	TodoPriority2 = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).Bold(true)

	TodoPriority3 = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226"))

	TodoPriority4 = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75"))

	PendingIconStyle = lipgloss.NewStyle().
				Foreground(colorPendingIcon)

	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("69")).
		Padding(0, 1)

	Dim = lipgloss.NewStyle().Foreground(colorDone)

	ModeInsert = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Bold(true)

	ModeNormal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	SavedIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Italic(true)
)

// Border returns the appropriate border style based on focus state.
func Border(active bool) lipgloss.Style {
	if active {
		return borderActive
	}
	return borderNormal
}

// BorderState returns the border style based on active and hovered state.
func BorderState(active, hovered bool) lipgloss.Style {
	switch {
	case active:
		return borderActive
	case hovered:
		return borderHovered
	default:
		return borderNormal
	}
}

// PendingIcon returns the rendered pending indicator.
func PendingIcon() string {
	return PendingIconStyle.Render("●")
}

// PriorityStyle returns the lipgloss style for the given priority level (1-4).
func PriorityStyle(p int) lipgloss.Style {
	switch p {
	case 1:
		return TodoPriority1
	case 2:
		return TodoPriority2
	case 3:
		return TodoPriority3
	default:
		return TodoPriority4
	}
}
