package history

import (
	"testing"
)

func TestHistory_Add(t *testing.T) {
	h := New(100)
	h.Add("entry1")
	h.Add("entry2")
	if h.Size() != 2 {
		t.Errorf("want size 2, got %d", h.Size())
	}
}

func TestHistory_Older(t *testing.T) {
	h := New(100)
	h.Add("first")
	h.Add("second")
	h.Add("third")
	entry, ok := h.Older()
	if !ok {
		t.Fatal("Older should return ok")
	}
	if entry != "third" {
		t.Errorf("want third, got %q", entry)
	}
	entry, ok = h.Older()
	if !ok {
		t.Fatal("Older should return ok")
	}
	if entry != "second" {
		t.Errorf("want second, got %q", entry)
	}
}

func TestHistory_Older_AtBeginning(t *testing.T) {
	h := New(100)
	h.Add("only")
	h.cursor = 0
	_, ok := h.Older()
	if ok {
		t.Error("Older should return false at beginning")
	}
}

func TestHistory_Newer(t *testing.T) {
	h := New(100)
	h.Add("first")
	h.Add("second")
	// After adding 2 entries, cursor = 2 (past end)
	entry, ok := h.Newer()
	if ok {
		t.Fatal("Newer at end should return false")
	}
	if entry != "" {
		t.Errorf("want empty, got %q", entry)
	}
	// Move to index 0 (showing "first"), Newer returns "second"
	h.cursor = 0
	entry, ok = h.Newer()
	if !ok {
		t.Fatal("Newer should return ok")
	}
	if entry != "second" {
		t.Errorf("want second, got %q", entry)
	}
	// Now at index 1 (showing "second"), Newer moves past end
	entry, ok = h.Newer()
	if !ok {
		t.Fatal("Newer past end should return ok with empty")
	}
	if entry != "" {
		t.Errorf("want empty, got %q", entry)
	}
	// Now truly at end
	entry, ok = h.Newer()
	if ok {
		t.Fatal("Newer at absolute end should return false")
	}
}

func TestHistory_AtNewest(t *testing.T) {
	h := New(100)
	h.Add("entry")
	// cursor is set to len(entries) by Add, so AtNewest = true
	if !h.AtNewest() {
		t.Error("should be at newest after adding")
	}
	h.cursor = 0
	if h.AtNewest() {
		t.Error("should not be at newest when cursor < len")
	}
}

func TestHistory_MaxSize(t *testing.T) {
	h := New(3)
	h.Add("a")
	h.Add("b")
	h.Add("c")
	h.Add("d")
	if h.Size() != 3 {
		t.Errorf("want size 3, got %d", h.Size())
	}
	entries := h.Entries()
	if entries[0] != "b" {
		t.Errorf("want oldest 'b', got %q", entries[0])
	}
}

func TestHistory_EmptyEntries(t *testing.T) {
	h := New(100)
	entries := h.Entries()
	if len(entries) != 0 {
		t.Errorf("want 0 entries, got %d", len(entries))
	}
}

func TestHistory_EntriesCopy(t *testing.T) {
	h := New(100)
	h.Add("entry")
	entries := h.Entries()
	entries[0] = "modified"
	if h.Entries()[0] != "entry" {
		t.Error("entries should return a copy")
	}
}
