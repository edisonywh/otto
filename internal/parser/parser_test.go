package parser

import (
	"testing"
	"time"

	"otto/internal/models"
)

var testDay = time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
var pastDay = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func TestParseTodos_Basic(t *testing.T) {
	content := "# Notes\n[] buy milk\n[x] done task\nsome prose"
	todos := ParseTodos(content, testDay)

	if len(todos) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(todos))
	}
	if todos[0].Text != "buy milk" || todos[0].Done {
		t.Errorf("unexpected todo[0]: %+v", todos[0])
	}
	if todos[1].Text != "done task" || !todos[1].Done {
		t.Errorf("unexpected todo[1]: %+v", todos[1])
	}
}

func TestParseTodos_Indented(t *testing.T) {
	content := "  [] indented todo\n\t[x] tab indented done"
	todos := ParseTodos(content, testDay)
	if len(todos) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(todos))
	}
	if todos[0].Text != "indented todo" {
		t.Errorf("unexpected text: %q", todos[0].Text)
	}
}

func TestParseTodos_EmptyTextIgnored(t *testing.T) {
	content := "[] \n[x] \n[] valid"
	todos := ParseTodos(content, testDay)
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(todos))
	}
}

func TestParseTodos_LineNumbers(t *testing.T) {
	content := "line1\n[] task\nline3"
	todos := ParseTodos(content, testDay)
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(todos))
	}
	if todos[0].LineNum != 2 {
		t.Errorf("expected LineNum 2, got %d", todos[0].LineNum)
	}
}

func TestToggleLine_IncompleteToComplete(t *testing.T) {
	content := "# Notes\n[] buy milk\nsome text"
	result := ToggleLine(content, 2)
	expected := "# Notes\n[x] buy milk\nsome text"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestToggleLine_CompleteToIncomplete(t *testing.T) {
	content := "# Notes\n[x] done\nsome text"
	result := ToggleLine(content, 2)
	expected := "# Notes\n[] done\nsome text"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestToggleLine_NonTodoLine(t *testing.T) {
	content := "# Notes\nsome prose\n[] task"
	result := ToggleLine(content, 2)
	if result != content {
		t.Errorf("expected content unchanged, got %q", result)
	}
}

func TestToggleLine_OutOfBounds(t *testing.T) {
	content := "line1"
	result := ToggleLine(content, 99)
	if result != content {
		t.Errorf("expected content unchanged on out-of-bounds")
	}
}

func TestIsOverdue(t *testing.T) {
	todos := ParseTodos("[] old task", pastDay)
	if len(todos) == 0 {
		t.Fatal("expected todo")
	}
	if !todos[0].IsOverdue() {
		t.Error("expected todo to be overdue")
	}
}

func TestCollectAllTodos_Order(t *testing.T) {
	n1 := &models.Note{Day: pastDay}
	n1.Todos = ParseTodos("[] overdue task", pastDay)

	n2 := &models.Note{Day: testDay}
	n2.Todos = ParseTodos("[] pending\n[x] done", testDay)

	all := CollectAllTodos([]*models.Note{n1, n2})
	if len(all) != 3 {
		t.Fatalf("expected 3 todos, got %d", len(all))
	}
	// First should be overdue
	if !all[0].IsOverdue() {
		t.Error("first todo should be overdue")
	}
	// Last should be done
	if !all[2].Done {
		t.Error("last todo should be done")
	}
}
