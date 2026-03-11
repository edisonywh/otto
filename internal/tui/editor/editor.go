package editor

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"otto/internal/models"
	"otto/internal/tui/styles"
	"otto/internal/tui/vim"
)

// VimMode represents the current editor mode.
type VimMode int

const (
	ModeInsert     VimMode = iota // Editable textarea
	ModeNormal                    // Vim navigation; textarea still visible + focused (cursor shown)
	ModePreview                   // Glamour read-only viewport (toggle with Ctrl+P)
	ModeVisualLine                // Visual line selection (V)
)

const maxUndoStack = 100

// snapshot captures content and cursor position for undo/redo.
type snapshot struct {
	content string
	line    int
	col     int
}

// Model is the daily notes editor pane.
type Model struct {
	textarea textarea.Model
	viewport viewport.Model
	note     *models.Note
	mode     VimMode
	prevMode VimMode // mode active before entering Preview

	// Undo / redo
	undoStack []snapshot
	redoStack []snapshot

	// Insert-session tracking: the entire insert session is one undo entry.
	insertStartContent string
	insertStartLine    int
	insertStartCol     int

	// Visual line mode anchor
	visualAnchor int // line where V was pressed

	// Pending operator / multi-key sequences
	pendingOp      byte // 'd', 'y', 'c', '>', '<', or 0
	pendingG       bool // first 'g' of 'gg'
	pendingReplace bool // 'r' waiting for replacement char
	pendingFind    bool // 'f'/'F' waiting for target char
	findForward    bool // direction of last f/F find
	findChar       rune // char of last f/F find (0 = none)
	pendingInner   bool // 'i' in ciw/diw/yiw

	// / search
	searching        bool
	searchQuery      string
	searchMatches    []vim.Position
	searchMatchIdx   int
	preFindCursorPos vim.Position // cursor pos before search started

	// Count prefix accumulator ("2" in "2j", "12" in "12w")
	countStr string

	yankBuffer string
	showSaved  bool
	active     bool
	hovered    bool
	width      int
	height     int
}

// New creates a new editor model.
func New() Model {
	ta := textarea.New()
	ta.Placeholder = "Start writing your notes for today...\nUse '[] task' to add a todo."
	ta.ShowLineNumbers = true
	ta.CharLimit = 0
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()

	return Model{
		textarea: ta,
		viewport: viewport.New(0, 0),
		mode:     ModePreview,
		prevMode: ModePreview,
	}
}

// SetNote loads a note into the editor.
func (m *Model) SetNote(note *models.Note) {
	m.note = note
	m.textarea.SetValue(note.Content)
	if m.active {
		m.textarea.Focus()
	}
	m.updateViewport()
}

// SetSize sets the editor dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	innerH := h - 5
	if innerH < 1 {
		innerH = 1
	}
	m.textarea.SetWidth(w - 4)
	m.textarea.SetHeight(innerH)
	m.viewport.Width = w - 4
	m.viewport.Height = innerH
}

// SetActive sets focus state. Textarea stays focused in Normal mode so cursor is visible.
func (m *Model) SetActive(active bool) {
	m.active = active
	if active {
		// Always enter Normal mode when the editor gains focus.
		m.mode = ModeNormal
		m.textarea.Focus()
	} else {
		// Always show the rendered preview when the editor loses focus.
		m.textarea.Blur()
		m.updateViewport()
		m.mode = ModePreview
	}
}

// SetHovered sets the hovered state.
func (m *Model) SetHovered(hovered bool) {
	m.hovered = hovered
}

// SetSaved controls the "Saved" indicator in the status line.
func (m *Model) SetSaved(show bool) {
	m.showSaved = show
}

// Mode returns the current vim mode.
func (m Model) Mode() VimMode {
	return m.mode
}

// EnterInsert forces the editor into Insert mode.
func (m *Model) EnterInsert() {
	m.mode = ModeInsert
	m.textarea.Focus()
	m.insertStartContent = m.textarea.Value()
	m.insertStartLine = m.textarea.Line()
	m.insertStartCol = m.textarea.LineInfo().CharOffset
	m.pendingOp = 0
	m.pendingG = false
	m.pendingReplace = false
	m.pendingFind = false
	m.pendingInner = false
	m.countStr = ""
}

// Content returns the current text in the editor.
func (m Model) Content() string { return m.textarea.Value() }

// Note returns the active note.
func (m Model) Note() *models.Note { return m.note }

// ── Internal mode transitions ─────────────────────────────────────────────────

func (m *Model) enterNormal() {
	// Push undo if content changed during the Insert session.
	if m.textarea.Value() != m.insertStartContent {
		if len(m.undoStack) >= maxUndoStack {
			m.undoStack = m.undoStack[1:]
		}
		m.undoStack = append(m.undoStack, snapshot{
			content: m.insertStartContent,
			line:    m.insertStartLine,
			col:     m.insertStartCol,
		})
		m.redoStack = nil
	}
	m.mode = ModeNormal
	m.pendingOp = 0
	m.pendingG = false
	m.pendingReplace = false
	m.pendingFind = false
	m.pendingInner = false
	m.countStr = ""
}

func (m *Model) enterInsert() {
	m.mode = ModeInsert
	m.insertStartContent = m.textarea.Value()
	m.insertStartLine = m.textarea.Line()
	m.insertStartCol = m.textarea.LineInfo().CharOffset
	if m.active {
		m.textarea.Focus()
	}
	m.pendingOp = 0
	m.pendingG = false
	m.pendingReplace = false
	m.pendingFind = false
	m.pendingInner = false
	m.countStr = ""
}

// todoLinePrefixes are the markers that start a todo line.
var todoLinePrefixes = []string{"[] ", "[x] ", "[1] ", "[2] ", "[3] ", "[4] "}

func isTodoLine(trimmed string) bool {
	for _, p := range todoLinePrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func (m *Model) updateViewport() {
	content := m.textarea.Value()
	// Glamour collapses consecutive lines into one paragraph. Insert a blank
	// line after each todo line so they render on separate lines.
	lines := strings.Split(content, "\n")
	processed := make([]string, 0, len(lines))
	for i, line := range lines {
		processed = append(processed, line)
		if isTodoLine(strings.TrimLeft(line, " \t")) {
			if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != "" {
				processed = append(processed, "")
			}
		}
	}
	content = strings.Join(processed, "\n")
	// Ensure content ends with newline so glamour closes the last block.
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	wordWrap := m.viewport.Width
	if wordWrap <= 0 {
		wordWrap = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(wordWrap),
	)
	if err != nil {
		m.viewport.SetContent(content)
		return
	}
	rendered, err := r.Render(content)
	if err != nil {
		rendered = content
	}
	m.viewport.SetContent(rendered)
}

// ── Undo / redo ───────────────────────────────────────────────────────────────

func (m *Model) pushUndo() {
	if len(m.undoStack) >= maxUndoStack {
		m.undoStack = m.undoStack[1:]
	}
	m.undoStack = append(m.undoStack, snapshot{
		content: m.textarea.Value(),
		line:    m.textarea.Line(),
		col:     m.textarea.LineInfo().CharOffset,
	})
	m.redoStack = nil
}

// ── Pending / count helpers ───────────────────────────────────────────────────

func (m *Model) resetPending() {
	m.pendingOp = 0
	m.pendingG = false
	m.pendingReplace = false
	m.pendingFind = false
	m.pendingInner = false
	m.countStr = ""
}

// parseCount returns the accumulated count (default 1) and clears countStr.
func (m *Model) parseCount() int {
	if m.countStr == "" {
		return 1
	}
	n, err := strconv.Atoi(m.countStr)
	m.countStr = ""
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

// ── Cursor helpers ────────────────────────────────────────────────────────────

func curPos(ta textarea.Model) vim.Position {
	return vim.Position{Line: ta.Line(), Col: ta.LineInfo().CharOffset}
}

func moveCursorToLine(ta textarea.Model, targetLine int) textarea.Model {
	for ta.Line() < targetLine {
		prevLine := ta.Line()
		prevRow := ta.LineInfo().RowOffset
		ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown})
		// Break only when truly stuck (line AND visual row both unchanged).
		// When traversing a wrapped logical line, ta.Line() stays the same but
		// RowOffset advances — that is not "stuck".
		if ta.Line() == prevLine && ta.LineInfo().RowOffset == prevRow {
			break
		}
	}
	for ta.Line() > targetLine {
		prevLine := ta.Line()
		prevRow := ta.LineInfo().RowOffset
		ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyUp})
		if ta.Line() == prevLine && ta.LineInfo().RowOffset == prevRow {
			break
		}
	}
	return ta
}

func navigateTo(ta textarea.Model, pos vim.Position) textarea.Model {
	ta = moveCursorToLine(ta, pos.Line)
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyHome})
	for i := 0; i < pos.Col; i++ {
		prev := ta.LineInfo().CharOffset
		ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyRight})
		if ta.LineInfo().CharOffset == prev {
			break
		}
	}
	return ta
}

// ── Operator helpers ──────────────────────────────────────────────────────────

// applyOpForward applies the pending op over [curPos, targetPos).
// Pass inclusive=true for 'e'/'E' to include the target char.
func (m *Model) applyOpForward(targetPos vim.Position, inclusive bool) {
	lines := vim.Lines(m.textarea.Value())
	cp := curPos(m.textarea)
	from := vim.ToOffset(lines, cp)
	to := vim.ToOffset(lines, targetPos)
	if inclusive {
		to++
	}
	switch m.pendingOp {
	case 'd':
		m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
		m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, to))
		m.textarea = navigateTo(m.textarea, cp)
	case 'y':
		m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
	case 'c':
		m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
		m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, to))
		m.textarea = navigateTo(m.textarea, cp)
		m.enterInsert()
	}
}

// applyOpBackward applies the pending op over [targetPos, curPos).
func (m *Model) applyOpBackward(targetPos vim.Position) {
	lines := vim.Lines(m.textarea.Value())
	cp := curPos(m.textarea)
	from := vim.ToOffset(lines, targetPos)
	to := vim.ToOffset(lines, cp)
	switch m.pendingOp {
	case 'd':
		m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
		m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, to))
		m.textarea = navigateTo(m.textarea, targetPos)
	case 'y':
		m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
	case 'c':
		m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
		m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, to))
		m.textarea = navigateTo(m.textarea, targetPos)
		m.enterInsert()
	}
}

// deleteLines deletes lines [from, to] inclusive and positions cursor at `from`.
func (m *Model) deleteLines(from, to int) {
	lines := vim.Lines(m.textarea.Value())
	if from > to {
		from, to = to, from
	}
	from = clampInt(from, 0, len(lines)-1)
	to = clampInt(to, 0, len(lines)-1)
	m.setYank(strings.Join(lines[from:to+1], "\n"))
	newLines := make([]string, 0, len(lines)-(to-from+1))
	newLines = append(newLines, lines[:from]...)
	newLines = append(newLines, lines[to+1:]...)
	if len(newLines) == 0 {
		newLines = []string{""}
	}
	targetLine := clampInt(from, 0, len(newLines)-1)
	m.textarea.SetValue(strings.Join(newLines, "\n"))
	// SetValue always resets cursor to line 0 (via Reset). Navigate to targetLine,
	// but first overshoot by half the viewport height so the cursor ends up near
	// the middle of the visible area rather than stuck at the bottom.
	h := m.textarea.Height()
	overshoot := clampInt(targetLine+h/2, 0, len(newLines)-1)
	m.textarea = moveCursorToLine(m.textarea, overshoot)
	if overshoot != targetLine {
		m.textarea = moveCursorToLine(m.textarea, targetLine)
	}
}

func (m *Model) indentCurrentLine(delta int) {
	content := m.textarea.Value()
	lines := vim.Lines(content)
	line := m.textarea.Line()
	if line >= len(lines) {
		return
	}
	if delta > 0 {
		lines[line] = "\t" + lines[line]
	} else {
		switch {
		case strings.HasPrefix(lines[line], "\t"):
			lines[line] = lines[line][1:]
		case strings.HasPrefix(lines[line], "    "):
			lines[line] = lines[line][4:]
		case strings.HasPrefix(lines[line], " "):
			lines[line] = lines[line][1:]
		}
	}
	savedLine := line
	m.textarea.SetValue(strings.Join(lines, "\n"))
	m.textarea = moveCursorToLine(m.textarea, savedLine)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// setYank sets the yank buffer and also writes to the system clipboard.
func (m *Model) setYank(text string) {
	m.yankBuffer = text
	_ = clipboard.WriteAll(text)
}

// ── Find-char helpers ─────────────────────────────────────────────────────────

func isEditorWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// doFind scans the current line for findChar and moves cursor (or applies op).
func (m *Model) doFind(forward bool) {
	content := m.textarea.Value()
	lines := vim.Lines(content)
	line := m.textarea.Line()
	col := m.textarea.LineInfo().CharOffset
	if line >= len(lines) {
		return
	}
	lr := []rune(lines[line])
	targetCol := -1
	if forward {
		for i := col + 1; i < len(lr); i++ {
			if lr[i] == m.findChar {
				targetCol = i
				break
			}
		}
	} else {
		for i := col - 1; i >= 0; i-- {
			if lr[i] == m.findChar {
				targetCol = i
				break
			}
		}
	}
	if targetCol < 0 {
		return
	}
	targetPos := vim.Position{Line: line, Col: targetCol}
	if m.pendingOp != 0 {
		m.pushUndo()
		if forward {
			m.applyOpForward(targetPos, true) // inclusive like vim f
		} else {
			m.applyOpBackward(targetPos)
		}
	} else {
		m.textarea = navigateTo(m.textarea, targetPos)
	}
}

// ── Search helpers ────────────────────────────────────────────────────────────

func (m *Model) updateSearchMatches() {
	m.searchMatches = computeSearchMatches(m.textarea.Value(), m.searchQuery)
	if len(m.searchMatches) > 0 {
		m.searchMatchIdx = 0
		m.textarea = navigateTo(m.textarea, m.searchMatches[0])
	}
}

func computeSearchMatches(content, query string) []vim.Position {
	if query == "" {
		return nil
	}
	queryRunes := []rune(query)
	qLen := len(queryRunes)
	var matches []vim.Position
	for lineIdx, line := range strings.Split(content, "\n") {
		lineRunes := []rune(line)
		for i := 0; i <= len(lineRunes)-qLen; i++ {
			match := true
			for j := 0; j < qLen; j++ {
				if lineRunes[i+j] != queryRunes[j] {
					match = false
					break
				}
			}
			if match {
				matches = append(matches, vim.Position{Line: lineIdx, Col: i})
			}
		}
	}
	return matches
}

// ── Inner-word helper ─────────────────────────────────────────────────────────

// innerWordRange returns byte offsets [from, to) of the word under the cursor.
func innerWordRange(_ string, lines []string, pos vim.Position) (int, int) {
	if pos.Line >= len(lines) {
		return 0, 0
	}
	lr := []rune(lines[pos.Line])
	col := pos.Col
	if len(lr) == 0 || col >= len(lr) {
		return 0, 0
	}
	if !isEditorWordChar(lr[col]) {
		return 0, 0
	}
	start := col
	for start > 0 && isEditorWordChar(lr[start-1]) {
		start--
	}
	end := col + 1
	for end < len(lr) && isEditorWordChar(lr[end]) {
		end++
	}
	lineStart := vim.ToOffset(lines, vim.Position{Line: pos.Line, Col: 0})
	return lineStart + start, lineStart + end
}

// Update handles keyboard events.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd

	// ── Preview mode ──────────────────────────────────────────────────────────
	if m.mode == ModePreview {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "ctrl+p", "esc":
				m.mode = m.prevMode
				if m.active {
					m.textarea.Focus()
				}
				return m, nil
			case "j", "down":
				m.viewport.LineDown(1)
				return m, nil
			case "k", "up":
				m.viewport.LineUp(1)
				return m, nil
			case "ctrl+d":
				m.viewport.HalfViewDown()
				return m, nil
			case "ctrl+u":
				m.viewport.HalfViewUp()
				return m, nil
			case "g":
				m.viewport.GotoTop()
				return m, nil
			case "G":
				m.viewport.GotoBottom()
				return m, nil
			default:
				return m, nil
			}
		}
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// ── Visual Line mode ──────────────────────────────────────────────────────
	if m.mode == ModeVisualLine {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			curLine := m.textarea.Line()
			from, to := m.visualAnchor, curLine
			if from > to {
				from, to = to, from
			}

			switch keyMsg.String() {
			case "esc", "V":
				m.mode = ModeNormal
				return m, nil
			case "j", "down":
				lines := vim.Lines(m.textarea.Value())
				if m.textarea.Line() < len(lines)-1 {
					m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyDown})
				}
				return m, nil
			case "k", "up":
				if m.textarea.Line() > 0 {
					m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyUp})
				}
				return m, nil
			case "y":
				lines := vim.Lines(m.textarea.Value())
				m.setYank(strings.Join(lines[from:to+1], "\n"))
				m.mode = ModeNormal
				return m, nil
			case "d", "x":
				m.pushUndo()
				m.deleteLines(from, to)
				m.mode = ModeNormal
				return m, nil
			case "c":
				m.pushUndo()
				m.deleteLines(from, to)
				m.enterInsert()
				return m, nil
			case "ctrl+p":
				m.updateViewport()
				m.prevMode = ModeNormal
				m.mode = ModePreview
				return m, nil
			}
		}
		return m, nil
	}

	// ── Normal mode ───────────────────────────────────────────────────────────
	if m.mode == ModeNormal {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			key := keyMsg.String()

			// ── Search input mode (while typing query) ────────────────────
			if m.searching {
				switch key {
				case "enter":
					m.searching = false
					return m, nil
				case "esc":
					m.searching = false
					m.searchQuery = ""
					m.searchMatches = nil
					m.textarea = navigateTo(m.textarea, m.preFindCursorPos)
					return m, nil
				case "backspace", "ctrl+h":
					if len(m.searchQuery) > 0 {
						runes := []rune(m.searchQuery)
						m.searchQuery = string(runes[:len(runes)-1])
						m.updateSearchMatches()
					}
					return m, nil
				default:
					runes := []rune(key)
					if len(runes) == 1 {
						m.searchQuery += string(runes)
						m.updateSearchMatches()
					}
					return m, nil
				}
			}

			// ── r<char>: replace char at cursor ──────────────────────────
			if m.pendingReplace {
				m.pendingReplace = false
				runes := []rune(key)
				if len(runes) == 1 {
					content := m.textarea.Value()
					lines := vim.Lines(content)
					line := m.textarea.Line()
					col := m.textarea.LineInfo().CharOffset
					if line < len(lines) {
						lr := []rune(lines[line])
						if col < len(lr) {
							m.pushUndo()
							savedPos := vim.Position{Line: line, Col: col}
							lr[col] = runes[0]
							lines[line] = string(lr)
							m.textarea.SetValue(strings.Join(lines, "\n"))
							m.textarea = navigateTo(m.textarea, savedPos)
						}
					}
				}
				return m, nil
			}

			// ── f/F pending: capture char to find ────────────────────────
			if m.pendingFind {
				runes := []rune(key)
				if len(runes) == 1 {
					m.findChar = runes[0]
					m.doFind(m.findForward)
				}
				m.resetPending()
				return m, nil
			}

			// ── Accumulate count prefix (1-9, or 0 after a leading digit) ─
			// Also accumulates when pendingG=true to support g{N}g navigation.
			if len(key) == 1 {
				ch := key[0]
				if ch >= '1' && ch <= '9' {
					m.countStr += key
					return m, nil
				}
				if ch == '0' && m.countStr != "" {
					m.countStr += "0"
					return m, nil
				}
			}

			switch key {

			// ── Preview toggle ────────────────────────────────────────────
			case "ctrl+p":
				m.updateViewport()
				m.prevMode = ModeNormal
				m.mode = ModePreview
				return m, nil

			// ── Enter insert mode ─────────────────────────────────────────
			case "i":
				if m.pendingOp != 0 {
					m.pendingInner = true
					return m, nil
				}
				m.enterInsert()
				return m, nil

			case "a":
				// Move one right within the current line (don't cross to next line).
				lines := vim.Lines(m.textarea.Value())
				line := m.textarea.Line()
				col := m.textarea.LineInfo().CharOffset
				if line < len(lines) && col < len([]rune(lines[line])) {
					m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: col + 1})
				}
				m.enterInsert()
				return m, nil

			case "I":
				m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyHome})
				m.enterInsert()
				return m, nil

			case "A":
				// KeyEnd overshoots to the next line when already at the last char.
				// Navigate explicitly to the last char, then move one right (append pos).
				lines := vim.Lines(m.textarea.Value())
				line := m.textarea.Line()
				if line < len(lines) {
					lineRunes := []rune(lines[line])
					if len(lineRunes) > 0 {
						m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: len(lineRunes) - 1})
					}
				}
				m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRight})
				m.enterInsert()
				return m, nil

			// ── Open line ─────────────────────────────────────────────────
			case "o":
				m.pushUndo()
				content := m.textarea.Value()
				lines := vim.Lines(content)
				line := clampInt(curPos(m.textarea).Line, 0, len(lines)-1)
				newLines := make([]string, 0, len(lines)+1)
				newLines = append(newLines, lines[:line+1]...)
				newLines = append(newLines, "")
				newLines = append(newLines, lines[line+1:]...)
				m.textarea.SetValue(strings.Join(newLines, "\n"))
				m.textarea = navigateTo(m.textarea, vim.Position{Line: line + 1, Col: 0})
				m.enterInsert()
				return m, nil

			case "O":
				m.pushUndo()
				content := m.textarea.Value()
				lines := vim.Lines(content)
				line := clampInt(curPos(m.textarea).Line, 0, len(lines)-1)
				newLines := make([]string, 0, len(lines)+1)
				newLines = append(newLines, lines[:line]...)
				newLines = append(newLines, "")
				newLines = append(newLines, lines[line:]...)
				m.textarea.SetValue(strings.Join(newLines, "\n"))
				m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: 0})
				m.enterInsert()
				return m, nil

			// ── j / k: line navigation (supports count + operator) ────────
			case "j", "down":
				count := m.parseCount()
				if m.pendingOp == 'd' || m.pendingOp == 'y' || m.pendingOp == 'c' {
					lines := vim.Lines(m.textarea.Value())
					startLine := m.textarea.Line()
					endLine := clampInt(startLine+count, 0, len(lines)-1)
					switch m.pendingOp {
					case 'd':
						m.pushUndo()
						m.deleteLines(startLine, endLine)
					case 'y':
						m.setYank(strings.Join(lines[startLine:endLine+1], "\n"))
					case 'c':
						m.pushUndo()
						m.deleteLines(startLine, endLine)
						m.enterInsert()
						return m, nil
					}
					m.resetPending()
				} else {
					lines := vim.Lines(m.textarea.Value())
					for i := 0; i < count && m.textarea.Line() < len(lines)-1; i++ {
						m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyDown})
					}
					m.resetPending()
				}
				return m, nil

			case "k", "up":
				count := m.parseCount()
				if m.pendingOp == 'd' || m.pendingOp == 'y' || m.pendingOp == 'c' {
					lines := vim.Lines(m.textarea.Value())
					startLine := m.textarea.Line()
					endLine := clampInt(startLine-count, 0, len(lines)-1)
					switch m.pendingOp {
					case 'd':
						m.pushUndo()
						m.deleteLines(endLine, startLine)
					case 'y':
						m.setYank(strings.Join(lines[endLine:startLine+1], "\n"))
					case 'c':
						m.pushUndo()
						m.deleteLines(endLine, startLine)
						m.enterInsert()
						return m, nil
					}
					m.resetPending()
				} else {
					for i := 0; i < count && m.textarea.Line() > 0; i++ {
						m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyUp})
					}
					m.resetPending()
				}
				return m, nil

			// ── h / l: character navigation ───────────────────────────────
			case "h":
				count := m.parseCount()
				for i := 0; i < count; i++ {
					m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyLeft})
				}
				m.resetPending()
				return m, nil

			case "l":
				count := m.parseCount()
				for i := 0; i < count; i++ {
					m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRight})
				}
				m.resetPending()
				return m, nil

			// ── Line start / end ──────────────────────────────────────────
			case "0":
				if m.pendingOp != 0 {
					lines := vim.Lines(m.textarea.Value())
					cp := curPos(m.textarea)
					lineStart := vim.Position{Line: cp.Line, Col: 0}
					from := vim.ToOffset(lines, lineStart)
					to := vim.ToOffset(lines, cp)
					switch m.pendingOp {
					case 'd':
						m.pushUndo()
						m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
						m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, to))
						m.textarea = navigateTo(m.textarea, lineStart)
					case 'y':
						m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
					case 'c':
						m.pushUndo()
						m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
						m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, to))
						m.textarea = navigateTo(m.textarea, lineStart)
						m.enterInsert()
						return m, nil
					}
					m.resetPending()
					return m, nil
				}
				m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyHome})
				m.resetPending()
				return m, nil

			case "$":
				if m.pendingOp != 0 {
					lines := vim.Lines(m.textarea.Value())
					cp := curPos(m.textarea)
					lineEndOff := vim.LineEnd(lines, cp.Line)
					from := vim.ToOffset(lines, cp)
					switch m.pendingOp {
					case 'd':
						m.pushUndo()
						m.setYank(vim.ExtractRange(m.textarea.Value(), from, lineEndOff))
						m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, lineEndOff))
						m.textarea = navigateTo(m.textarea, cp)
					case 'y':
						m.setYank(vim.ExtractRange(m.textarea.Value(), from, lineEndOff))
					case 'c':
						m.pushUndo()
						m.setYank(vim.ExtractRange(m.textarea.Value(), from, lineEndOff))
						m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, lineEndOff))
						m.textarea = navigateTo(m.textarea, cp)
						m.enterInsert()
						return m, nil
					}
					m.resetPending()
					return m, nil
				}
				m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyEnd})
				m.resetPending()
				return m, nil

			// ── Half-page scroll ──────────────────────────────────────────
			case "ctrl+d":
				lines := vim.Lines(m.textarea.Value())
				steps := m.viewport.Height / 2
				if steps < 1 {
					steps = 5
				}
				for i := 0; i < steps && m.textarea.Line() < len(lines)-1; i++ {
					m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyDown})
				}
				m.resetPending()
				return m, nil

			case "ctrl+u":
				steps := m.viewport.Height / 2
				if steps < 1 {
					steps = 5
				}
				for i := 0; i < steps && m.textarea.Line() > 0; i++ {
					m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyUp})
				}
				m.resetPending()
				return m, nil

			// ── g / G ─────────────────────────────────────────────────────
			// {N}gg or g{N}g: go to line N (1-indexed). gg alone → top.
			case "g":
				if m.pendingG {
					count := m.parseCount()
					lines := vim.Lines(m.textarea.Value())
					targetLine := 0
					if count > 1 {
						targetLine = clampInt(count-1, 0, len(lines)-1)
					}
					if m.pendingOp == 'd' || m.pendingOp == 'y' || m.pendingOp == 'c' {
						startLine := m.textarea.Line()
						from, to := startLine, targetLine
						if from > to {
							from, to = to, from
						}
						switch m.pendingOp {
						case 'd':
							m.pushUndo()
							m.deleteLines(from, to)
						case 'y':
							m.setYank(strings.Join(lines[from:to+1], "\n"))
						case 'c':
							m.pushUndo()
							m.deleteLines(from, to)
							m.pendingG = false
							m.resetPending()
							m.enterInsert()
							return m, nil
						}
					} else {
						m.textarea = moveCursorToLine(m.textarea, targetLine)
						m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyHome})
					}
					m.pendingG = false
					m.resetPending()
				} else {
					// preserve countStr for g{N}g and pendingOp for dgg/ygg
					m.pendingG = true
				}
				return m, nil

			case "G":
				lines := vim.Lines(m.textarea.Value())
				targetLine := len(lines) - 1
				if targetLine < 0 {
					targetLine = 0
				}
				if m.pendingOp == 'd' || m.pendingOp == 'y' || m.pendingOp == 'c' {
					startLine := m.textarea.Line()
					switch m.pendingOp {
					case 'd':
						m.pushUndo()
						m.deleteLines(startLine, targetLine)
					case 'y':
						m.setYank(strings.Join(lines[startLine:targetLine+1], "\n"))
					case 'c':
						m.pushUndo()
						m.deleteLines(startLine, targetLine)
						m.resetPending()
						m.enterInsert()
						return m, nil
					}
				} else {
					m.textarea = moveCursorToLine(m.textarea, targetLine)
					m.textarea, _ = m.textarea.Update(tea.KeyMsg{Type: tea.KeyHome})
				}
				m.resetPending()
				return m, nil

			// ── Word motions: w b e W B E (with count and operator) ───────
			case "w":
				if m.pendingInner && m.pendingOp != 0 {
					// ciw / diw / yiw
					lines := vim.Lines(m.textarea.Value())
					from, to := innerWordRange(m.textarea.Value(), lines, curPos(m.textarea))
					if from < to {
						m.pushUndo()
						m.setYank(vim.ExtractRange(m.textarea.Value(), from, to))
						if m.pendingOp == 'd' || m.pendingOp == 'c' {
							m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, to))
							newLines := vim.Lines(m.textarea.Value())
							startPos := vim.FromOffset(newLines, from)
							m.textarea = navigateTo(m.textarea, startPos)
							if m.pendingOp == 'c' {
								m.resetPending()
								m.enterInsert()
								return m, nil
							}
						}
					}
					m.resetPending()
					return m, nil
				}
				count := m.parseCount()
				lines := vim.Lines(m.textarea.Value())
				np := curPos(m.textarea)
				for i := 0; i < count; i++ {
					np = vim.WordForward(lines, np)
				}
				if m.pendingOp != 0 {
					m.pushUndo()
					m.applyOpForward(np, false)
					m.resetPending()
				} else {
					m.textarea = navigateTo(m.textarea, np)
					m.resetPending()
				}
				return m, nil

			case "b":
				count := m.parseCount()
				lines := vim.Lines(m.textarea.Value())
				np := curPos(m.textarea)
				for i := 0; i < count; i++ {
					np = vim.WordBack(lines, np)
				}
				if m.pendingOp != 0 {
					m.pushUndo()
					m.applyOpBackward(np)
					m.resetPending()
				} else {
					m.textarea = navigateTo(m.textarea, np)
					m.resetPending()
				}
				return m, nil

			case "e":
				count := m.parseCount()
				lines := vim.Lines(m.textarea.Value())
				np := curPos(m.textarea)
				for i := 0; i < count; i++ {
					np = vim.WordEnd(lines, np)
				}
				if m.pendingOp != 0 {
					m.pushUndo()
					m.applyOpForward(np, true) // inclusive
					m.resetPending()
				} else {
					m.textarea = navigateTo(m.textarea, np)
					m.resetPending()
				}
				return m, nil

			case "W":
				count := m.parseCount()
				lines := vim.Lines(m.textarea.Value())
				np := curPos(m.textarea)
				for i := 0; i < count; i++ {
					np = vim.WORDForward(lines, np)
				}
				if m.pendingOp != 0 {
					m.pushUndo()
					m.applyOpForward(np, false)
					m.resetPending()
				} else {
					m.textarea = navigateTo(m.textarea, np)
					m.resetPending()
				}
				return m, nil

			case "B":
				count := m.parseCount()
				lines := vim.Lines(m.textarea.Value())
				np := curPos(m.textarea)
				for i := 0; i < count; i++ {
					np = vim.WORDBack(lines, np)
				}
				if m.pendingOp != 0 {
					m.pushUndo()
					m.applyOpBackward(np)
					m.resetPending()
				} else {
					m.textarea = navigateTo(m.textarea, np)
					m.resetPending()
				}
				return m, nil

			case "E":
				count := m.parseCount()
				lines := vim.Lines(m.textarea.Value())
				np := curPos(m.textarea)
				for i := 0; i < count; i++ {
					np = vim.WORDEnd(lines, np)
				}
				if m.pendingOp != 0 {
					m.pushUndo()
					m.applyOpForward(np, true) // inclusive
					m.resetPending()
				} else {
					m.textarea = navigateTo(m.textarea, np)
					m.resetPending()
				}
				return m, nil

			// ── f/F: find char on line ────────────────────────────────────
			case "f":
				m.pendingFind = true
				m.findForward = true
				m.pendingG = false
				return m, nil

			case "F":
				m.pendingFind = true
				m.findForward = false
				m.pendingG = false
				return m, nil

			case ";":
				if m.findChar != 0 {
					m.doFind(m.findForward)
				}
				m.resetPending()
				return m, nil

			case ",":
				if m.findChar != 0 {
					m.doFind(!m.findForward)
				}
				m.resetPending()
				return m, nil

			// ── x: delete count chars forward ─────────────────────────────
			case "x":
				count := m.parseCount()
				content := m.textarea.Value()
				lines := vim.Lines(content)
				line := m.textarea.Line()
				col := m.textarea.LineInfo().CharOffset
				if line < len(lines) {
					lr := []rune(lines[line])
					end := col + count
					if end > len(lr) {
						end = len(lr)
					}
					if col < len(lr) {
						m.pushUndo()
						m.setYank(string(lr[col:end]))
						newLine := string(lr[:col]) + string(lr[end:])
						lines[line] = newLine
						m.textarea.SetValue(strings.Join(lines, "\n"))
						newCol := col
						newLen := len([]rune(newLine))
						if newLen == 0 {
							newCol = 0
						} else if newCol >= newLen {
							newCol = newLen - 1
						}
						m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: newCol})
					}
				}
				m.resetPending()
				return m, nil

			// ── s: substitute count chars (delete + insert) ───────────────
			case "s":
				count := m.parseCount()
				content := m.textarea.Value()
				lines := vim.Lines(content)
				line := m.textarea.Line()
				col := m.textarea.LineInfo().CharOffset
				if line < len(lines) {
					lr := []rune(lines[line])
					end := col + count
					if end > len(lr) {
						end = len(lr)
					}
					if col < len(lr) {
						m.pushUndo()
						m.setYank(string(lr[col:end]))
						lines[line] = string(lr[:col]) + string(lr[end:])
						m.textarea.SetValue(strings.Join(lines, "\n"))
						m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: col})
					}
				}
				m.enterInsert()
				return m, nil

			// ── r: replace char ───────────────────────────────────────────
			case "r":
				m.pendingOp = 0
				m.pendingG = false
				m.countStr = ""
				m.pendingReplace = true
				return m, nil

			// ── ~: toggle case ────────────────────────────────────────────
			case "~":
				content := m.textarea.Value()
				lines := vim.Lines(content)
				line := m.textarea.Line()
				col := m.textarea.LineInfo().CharOffset
				if line < len(lines) {
					lr := []rune(lines[line])
					if col < len(lr) {
						m.pushUndo()
						r := lr[col]
						if unicode.IsUpper(r) {
							lr[col] = unicode.ToLower(r)
						} else {
							lr[col] = unicode.ToUpper(r)
						}
						lines[line] = string(lr)
						nextCol := col + 1
						if nextCol >= len(lr) {
							nextCol = len(lr) - 1
						}
						if nextCol < 0 {
							nextCol = 0
						}
						m.textarea.SetValue(strings.Join(lines, "\n"))
						m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: nextCol})
					}
				}
				m.resetPending()
				return m, nil

			// ── J: join line below ────────────────────────────────────────
			case "J":
				content := m.textarea.Value()
				lines := vim.Lines(content)
				line := m.textarea.Line()
				if line < len(lines)-1 {
					m.pushUndo()
					joinCol := len([]rune(lines[line]))
					next := strings.TrimLeft(lines[line+1], " \t")
					if len(lines[line]) > 0 {
						lines[line] = lines[line] + " " + next
					} else {
						lines[line] = next
					}
					newLines := make([]string, 0, len(lines)-1)
					newLines = append(newLines, lines[:line+1]...)
					newLines = append(newLines, lines[line+2:]...)
					m.textarea.SetValue(strings.Join(newLines, "\n"))
					m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: joinCol})
				}
				m.resetPending()
				return m, nil

			// ── D: delete to end of line ──────────────────────────────────
			case "D":
				lines := vim.Lines(m.textarea.Value())
				cp := curPos(m.textarea)
				lineEndOff := vim.LineEnd(lines, cp.Line)
				from := vim.ToOffset(lines, cp)
				if from < lineEndOff {
					m.pushUndo()
					m.setYank(vim.ExtractRange(m.textarea.Value(), from, lineEndOff))
					m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, lineEndOff))
					m.textarea = navigateTo(m.textarea, cp)
				}
				m.resetPending()
				return m, nil

			// ── C: change to end of line ──────────────────────────────────
			case "C":
				lines := vim.Lines(m.textarea.Value())
				cp := curPos(m.textarea)
				lineEndOff := vim.LineEnd(lines, cp.Line)
				from := vim.ToOffset(lines, cp)
				if from < lineEndOff {
					m.pushUndo()
					m.setYank(vim.ExtractRange(m.textarea.Value(), from, lineEndOff))
					m.textarea.SetValue(vim.DeleteRange(m.textarea.Value(), from, lineEndOff))
					m.textarea = navigateTo(m.textarea, cp)
				}
				m.enterInsert()
				return m, nil

			// ── S: change whole line ──────────────────────────────────────
			case "S":
				content := m.textarea.Value()
				lines := vim.Lines(content)
				line := m.textarea.Line()
				if line < len(lines) {
					m.pushUndo()
					m.setYank(lines[line])
					lines[line] = ""
					m.textarea.SetValue(strings.Join(lines, "\n"))
					m.textarea = moveCursorToLine(m.textarea, line)
				}
				m.enterInsert()
				return m, nil

			// ── d: delete operator ────────────────────────────────────────
			case "d":
				if m.pendingOp == 'd' {
					count := m.parseCount()
					line := m.textarea.Line()
					lines := vim.Lines(m.textarea.Value())
					endLine := clampInt(line+count-1, 0, len(lines)-1)
					m.pushUndo()
					m.deleteLines(line, endLine)
					m.pendingOp = 0
				} else {
					m.pendingG = false
					m.pendingOp = 'd'
				}
				return m, nil

			// ── y: yank operator ──────────────────────────────────────────
			case "y":
				if m.pendingOp == 'y' {
					count := m.parseCount()
					content := m.textarea.Value()
					lines := vim.Lines(content)
					line := m.textarea.Line()
					endLine := clampInt(line+count-1, 0, len(lines)-1)
					m.setYank(strings.Join(lines[line:endLine+1], "\n"))
					m.pendingOp = 0
				} else {
					m.pendingG = false
					m.pendingOp = 'y'
				}
				return m, nil

			// ── c: change operator ────────────────────────────────────────
			case "c":
				if m.pendingOp == 'c' {
					count := m.parseCount()
					content := m.textarea.Value()
					lines := vim.Lines(content)
					line := m.textarea.Line()
					endLine := clampInt(line+count-1, 0, len(lines)-1)
					m.pushUndo()
					m.setYank(strings.Join(lines[line:endLine+1], "\n"))
					// Replace range with a single blank line at `line`
					newLines := make([]string, 0)
					newLines = append(newLines, lines[:line]...)
					newLines = append(newLines, "")
					newLines = append(newLines, lines[endLine+1:]...)
					m.textarea.SetValue(strings.Join(newLines, "\n"))
					m.textarea = moveCursorToLine(m.textarea, line)
					m.enterInsert()
				} else {
					m.pendingG = false
					m.pendingOp = 'c'
				}
				return m, nil

			// ── >>/<< indent / unindent ───────────────────────────────────
			case ">":
				if m.pendingOp == '>' {
					m.pushUndo()
					m.indentCurrentLine(1)
					m.resetPending()
				} else {
					m.pendingG = false
					m.countStr = ""
					m.pendingOp = '>'
				}
				return m, nil

			case "<":
				if m.pendingOp == '<' {
					m.pushUndo()
					m.indentCurrentLine(-1)
					m.resetPending()
				} else {
					m.pendingG = false
					m.countStr = ""
					m.pendingOp = '<'
				}
				return m, nil

			// ── p / P: paste ──────────────────────────────────────────────
			case "p":
				if m.yankBuffer != "" {
					m.pushUndo()
					content := m.textarea.Value()
					lines := vim.Lines(content)
					line := clampInt(m.textarea.Line(), 0, len(lines)-1)
					newLines := make([]string, 0, len(lines)+1)
					newLines = append(newLines, lines[:line+1]...)
					newLines = append(newLines, m.yankBuffer)
					newLines = append(newLines, lines[line+1:]...)
					m.textarea.SetValue(strings.Join(newLines, "\n"))
					m.textarea = moveCursorToLine(m.textarea, line+1)
				}
				m.resetPending()
				return m, nil

			case "P":
				if m.yankBuffer != "" {
					m.pushUndo()
					content := m.textarea.Value()
					lines := vim.Lines(content)
					line := clampInt(m.textarea.Line(), 0, len(lines))
					newLines := make([]string, 0, len(lines)+1)
					newLines = append(newLines, lines[:line]...)
					newLines = append(newLines, m.yankBuffer)
					newLines = append(newLines, lines[line:]...)
					m.textarea.SetValue(strings.Join(newLines, "\n"))
					m.textarea = moveCursorToLine(m.textarea, line)
				}
				m.resetPending()
				return m, nil

			// ── u: undo ───────────────────────────────────────────────────
			case "u":
				if len(m.undoStack) > 0 {
					cur := snapshot{
						content: m.textarea.Value(),
						line:    m.textarea.Line(),
						col:     m.textarea.LineInfo().CharOffset,
					}
					m.redoStack = append(m.redoStack, cur)
					prev := m.undoStack[len(m.undoStack)-1]
					m.undoStack = m.undoStack[:len(m.undoStack)-1]
					m.textarea.SetValue(prev.content)
					m.textarea = navigateTo(m.textarea, vim.Position{Line: prev.line, Col: prev.col})
				}
				m.resetPending()
				return m, nil

			// ── ctrl+r: redo ──────────────────────────────────────────────
			case "ctrl+r":
				if len(m.redoStack) > 0 {
					cur := snapshot{
						content: m.textarea.Value(),
						line:    m.textarea.Line(),
						col:     m.textarea.LineInfo().CharOffset,
					}
					m.undoStack = append(m.undoStack, cur)
					next := m.redoStack[len(m.redoStack)-1]
					m.redoStack = m.redoStack[:len(m.redoStack)-1]
					m.textarea.SetValue(next.content)
					m.textarea = navigateTo(m.textarea, vim.Position{Line: next.line, Col: next.col})
				}
				m.resetPending()
				return m, nil

			// ── /: start search ───────────────────────────────────────────
			case "/":
				m.searching = true
				m.searchQuery = ""
				m.preFindCursorPos = curPos(m.textarea)
				m.resetPending()
				return m, nil

			// ── n/N: next/prev search match ───────────────────────────────
			case "n":
				if len(m.searchMatches) > 0 {
					m.searchMatchIdx = (m.searchMatchIdx + 1) % len(m.searchMatches)
					m.textarea = navigateTo(m.textarea, m.searchMatches[m.searchMatchIdx])
				}
				m.resetPending()
				return m, nil

			case "N":
				if len(m.searchMatches) > 0 {
					m.searchMatchIdx = (m.searchMatchIdx - 1 + len(m.searchMatches)) % len(m.searchMatches)
					m.textarea = navigateTo(m.textarea, m.searchMatches[m.searchMatchIdx])
				}
				m.resetPending()
				return m, nil

			// ── V: visual line mode ────────────────────────────────────────
			case "V":
				m.visualAnchor = m.textarea.Line()
				m.mode = ModeVisualLine
				m.resetPending()
				return m, nil

			default:
				m.resetPending()
				return m, nil
			}
		}
		return m, cmd
	}

	// ── Insert mode ───────────────────────────────────────────────────────────
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+p":
			m.updateViewport()
			m.prevMode = ModeInsert
			m.mode = ModePreview
			return m, nil
		case "tab":
			// tea.KeyTab has empty Runes so the textarea default handler inserts
			// nothing. Insert the tab character explicitly.
			content := m.textarea.Value()
			lines := vim.Lines(content)
			line := m.textarea.Line()
			col := m.textarea.LineInfo().CharOffset
			if line < len(lines) {
				lr := []rune(lines[line])
				newLr := make([]rune, 0, len(lr)+1)
				newLr = append(newLr, lr[:col]...)
				newLr = append(newLr, '\t')
				newLr = append(newLr, lr[col:]...)
				lines[line] = string(newLr)
				m.textarea.SetValue(strings.Join(lines, "\n"))
				m.textarea = navigateTo(m.textarea, vim.Position{Line: line, Col: col + 1})
			}
			return m, nil
		}
		if keyMsg.Type == tea.KeyEsc {
			m.enterNormal() // pushes undo if content changed
			return m, nil
		}
	}

	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// View renders the editor pane.
func (m Model) View() string {
	border := styles.BorderState(m.active, m.hovered)

	title := ""
	if m.note != nil {
		title = styles.Title.Render("Notes — " + m.note.DateKey())
	} else {
		title = styles.Title.Render("Notes")
	}

	var content string
	switch m.mode {
	case ModePreview:
		content = m.viewport.View()
	case ModeVisualLine:
		content = m.renderVisualLines()
	default:
		content = m.textarea.View()
	}

	var statusLine string
	switch m.mode {
	case ModeInsert:
		statusLine = styles.ModeInsert.Render("-- INSERT --")
	case ModeNormal:
		if m.searching {
			statusLine = styles.ModeNormal.Render("/ " + m.searchQuery)
		} else if m.searchQuery != "" && len(m.searchMatches) > 0 {
			statusLine = styles.ModeNormal.Render(fmt.Sprintf("/ %s (%d/%d)", m.searchQuery, m.searchMatchIdx+1, len(m.searchMatches)))
		} else {
			statusLine = styles.ModeNormal.Render("-- NORMAL --")
		}
	case ModeVisualLine:
		curLine := m.textarea.Line()
		from, to := m.visualAnchor, curLine
		if from > to {
			from, to = to, from
		}
		statusLine = styles.ModeNormal.Render(fmt.Sprintf("-- VISUAL LINE -- (%d lines)", to-from+1))
	default:
		statusLine = styles.ModeNormal.Render("-- PREVIEW --")
	}
	if m.showSaved {
		statusLine += "  " + styles.SavedIndicator.Render("Saved")
	}

	inner := title + "\n" + content + "\n" + statusLine
	return border.Width(m.width - 2).Height(m.height - 2).Render(inner)
}

// renderVisualLines renders the editor content with selected lines highlighted.
func (m Model) renderVisualLines() string {
	lines := vim.Lines(m.textarea.Value())
	h := m.textarea.Height()
	cursorLine := m.textarea.Line()

	// Approximate viewport offset (mirrors bubbles textarea scroll behaviour).
	yOffset := 0
	if cursorLine >= h {
		yOffset = cursorLine - h + 1
	}
	end := yOffset + h
	if end > len(lines) {
		end = len(lines)
	}

	from, to := m.visualAnchor, cursorLine
	if from > to {
		from, to = to, from
	}

	numWidth := len(strconv.Itoa(len(lines)))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("255"))
	contentWidth := m.viewport.Width - numWidth - 4 // " N │ "

	var sb strings.Builder
	for i := yOffset; i < end; i++ {
		lineNum := fmt.Sprintf("%*d", numWidth, i+1)
		lineContent := lines[i]
		if contentWidth > 0 && len([]rune(lineContent)) > contentWidth {
			lineContent = string([]rune(lineContent)[:contentWidth])
		}
		row := lineNum + " │ " + lineContent
		if i >= from && i <= to {
			// Pad to full width so the background fills the line.
			w := m.viewport.Width - 2
			sb.WriteString(selStyle.Width(w).Render(row))
		} else {
			sb.WriteString(dimStyle.Render(lineNum) + " │ " + lineContent)
		}
		if i < end-1 {
			sb.WriteString("\n")
		}
	}
	// Pad with blank lines to fill textarea height.
	rendered := sb.String()
	linesRendered := end - yOffset
	for i := linesRendered; i < h; i++ {
		rendered += "\n"
	}
	return rendered
}
