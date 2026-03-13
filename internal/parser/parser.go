package parser

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"otto/internal/models"
)

// todoMarker detects the todo marker at the start of a trimmed line.
// Returns the marker string (e.g. "[] ", "[x] ", "[1] ") or "" if not a todo.
func todoMarker(trimmed string) (marker string, done bool, priority int) {
	switch {
	case strings.HasPrefix(trimmed, "[] "):
		return "[] ", false, 0
	case strings.HasPrefix(trimmed, "[x] "):
		return "[x] ", true, 0
	case strings.HasPrefix(trimmed, "[1] "):
		return "[1] ", false, 1
	case strings.HasPrefix(trimmed, "[2] "):
		return "[2] ", false, 2
	case strings.HasPrefix(trimmed, "[3] "):
		return "[3] ", false, 3
	case strings.HasPrefix(trimmed, "[4] "):
		return "[4] ", false, 4
	}
	return "", false, 0
}

// ParseTodos parses todos from the given content string for the given day.
func ParseTodos(content string, day time.Time) []models.Todo {
	var todos []models.Todo
	lines := strings.Split(content, "\n")
	dayStr := day.Format(models.DateFormat)

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		marker, done, priority := todoMarker(trimmed)
		if marker == "" {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
		if text == "" {
			continue
		}
		todos = append(todos, models.Todo{
			ID:       fmt.Sprintf("%s:%d", dayStr, i+1),
			Day:      day,
			Text:     text,
			Done:     done,
			LineNum:  i + 1,
			Priority: priority,
		})
	}

	return todos
}

// ToggleLine toggles the done state of a specific line in content.
// Priority markers ([1]-[4]) become [x] when done, and revert when undone.
func ToggleLine(content string, lineNum int) string {
	lines := strings.Split(content, "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return content
	}

	idx := lineNum - 1
	line := lines[idx]
	trimmed := strings.TrimLeft(line, " \t")
	leading := line[:len(line)-len(trimmed)]

	marker, done, priority := todoMarker(trimmed)
	if marker == "" {
		return content
	}

	text := strings.TrimPrefix(trimmed, marker)
	if done {
		// Mark undone: [x] → [] , preserve priority if it was stored
		lines[idx] = leading + "[] " + text
	} else if priority > 0 {
		// Priority pending → done (store priority in marker for round-trip)
		lines[idx] = leading + "[x] " + text
	} else {
		// Regular pending → done
		lines[idx] = leading + "[x] " + text
	}

	return strings.Join(lines, "\n")
}

// SetPriority sets or clears the priority on a todo line.
// priority 0 removes priority (reverts to []).
// Only acts on pending (non-done) todo lines.
func SetPriority(content string, lineNum int, priority int) string {
	lines := strings.Split(content, "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return content
	}

	idx := lineNum - 1
	line := lines[idx]
	trimmed := strings.TrimLeft(line, " \t")
	leading := line[:len(line)-len(trimmed)]

	marker, done, _ := todoMarker(trimmed)
	if marker == "" || done {
		return content
	}

	text := strings.TrimPrefix(trimmed, marker)
	var newMarker string
	if priority < 1 || priority > 4 {
		newMarker = "[] "
	} else {
		newMarker = "[" + strconv.Itoa(priority) + "] "
	}
	lines[idx] = leading + newMarker + text
	return strings.Join(lines, "\n")
}

// SetTodoText replaces the text of a todo line, preserving its marker.
func SetTodoText(content string, lineNum int, newText string) string {
	lines := strings.Split(content, "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return content
	}

	idx := lineNum - 1
	line := lines[idx]
	trimmed := strings.TrimLeft(line, " \t")
	leading := line[:len(line)-len(trimmed)]

	marker, _, _ := todoMarker(trimmed)
	if marker == "" {
		return content
	}

	lines[idx] = leading + marker + strings.TrimSpace(newText)
	return strings.Join(lines, "\n")
}

// CollectAllTodos merges todos from all notes, ordered: overdue → priority(1-4) → pending → done.
func CollectAllTodos(notes []*models.Note) []models.Todo {
	var overdue []models.Todo
	priority := [5][]models.Todo{} // index 1-4 used
	var pending []models.Todo
	var done []models.Todo

	for _, note := range notes {
		for _, t := range note.Todos {
			switch {
			case t.IsOverdue():
				overdue = append(overdue, t)
			case t.Done:
				done = append(done, t)
			case t.Priority >= 1 && t.Priority <= 4:
				priority[t.Priority] = append(priority[t.Priority], t)
			default:
				pending = append(pending, t)
			}
		}
	}

	sortByDay := func(todos []models.Todo) {
		sort.Slice(todos, func(i, j int) bool {
			return todos[i].Day.After(todos[j].Day)
		})
	}
	// Overdue: highest priority first (1 beats 2 beats … beats 0/no-priority), then newer date.
	sort.SliceStable(overdue, func(i, j int) bool {
		pi, pj := overdue[i].Priority, overdue[j].Priority
		if pi == 0 {
			pi = 5
		}
		if pj == 0 {
			pj = 5
		}
		if pi != pj {
			return pi < pj
		}
		return overdue[i].Day.After(overdue[j].Day)
	})
	for i := 1; i <= 4; i++ {
		sortByDay(priority[i])
	}
	sortByDay(pending)
	sortByDay(done)

	result := make([]models.Todo, 0)
	result = append(result, overdue...)
	for i := 1; i <= 4; i++ {
		result = append(result, priority[i]...)
	}
	result = append(result, pending...)
	result = append(result, done...)
	return result
}
