package outline

import (
	"errors"
	"testing"
)

func TestAdd_TopLevel(t *testing.T) {
	var o Outline
	n, err := o.Add("", "Market is shifting")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if n.ID == "" {
		t.Error("node has no ID")
	}
	if len(o.Nodes) != 1 {
		t.Fatalf("Nodes = %d, want 1", len(o.Nodes))
	}
}

func TestAdd_Child(t *testing.T) {
	var o Outline
	parent, _ := o.Add("", "Parent")
	child, err := o.Add(parent.ID, "Child")
	if err != nil {
		t.Fatalf("Add child: %v", err)
	}
	if len(parent.Children) != 1 || parent.Children[0].ID != child.ID {
		t.Errorf("child not attached: %+v", parent.Children)
	}
	if len(o.Nodes) != 1 {
		t.Errorf("child should not also be top-level; Nodes = %d", len(o.Nodes))
	}
}

func TestAdd_TrimsAndRejectsEmpty(t *testing.T) {
	var o Outline
	n, err := o.Add("", "  Padded  ")
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Padded" {
		t.Errorf("Title = %q, want trimmed", n.Title)
	}
	if _, err := o.Add("", "   "); !errors.Is(err, ErrEmptyTitle) {
		t.Errorf("err = %v, want ErrEmptyTitle", err)
	}
}

func TestAdd_UnknownParent(t *testing.T) {
	var o Outline
	if _, err := o.Add("n-missing", "Child"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAdd_IDsAreUnique(t *testing.T) {
	var o Outline
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		n, err := o.Add("", "point")
		if err != nil {
			t.Fatal(err)
		}
		if seen[n.ID] {
			t.Fatalf("duplicate node ID %q", n.ID)
		}
		seen[n.ID] = true
	}
}

func TestFindAndRemove_Nested(t *testing.T) {
	var o Outline
	a, _ := o.Add("", "A")
	b, _ := o.Add(a.ID, "B")
	c, _ := o.Add(b.ID, "C")

	if got := o.Find(c.ID); got == nil || got.Title != "C" {
		t.Fatalf("Find(C) = %+v", got)
	}
	if err := o.Remove(b.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if o.Find(b.ID) != nil {
		t.Error("B still present after removal")
	}
	if o.Find(c.ID) != nil {
		t.Error("C should have been removed with its parent")
	}
	if o.Find(a.ID) == nil {
		t.Error("A should survive")
	}
}

func TestRemove_NotFound(t *testing.T) {
	var o Outline
	if err := o.Remove("n-nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFlatten_DepthOrder(t *testing.T) {
	var o Outline
	a, _ := o.Add("", "A")
	_, _ = o.Add(a.ID, "A1")
	_, _ = o.Add("", "B")

	flat := o.Flatten()
	if len(flat) != 3 {
		t.Fatalf("Flatten len = %d, want 3", len(flat))
	}
	want := []struct {
		title string
		depth int
	}{{"A", 0}, {"A1", 1}, {"B", 0}}
	for i, w := range want {
		if flat[i].Node.Title != w.title || flat[i].Depth != w.depth {
			t.Errorf("flat[%d] = (%q, %d), want (%q, %d)",
				i, flat[i].Node.Title, flat[i].Depth, w.title, w.depth)
		}
	}
}

func TestWalk_EarlyStop(t *testing.T) {
	var o Outline
	_, _ = o.Add("", "A")
	_, _ = o.Add("", "B")
	_, _ = o.Add("", "C")

	visited := 0
	o.walk(func(*Node, int) bool {
		visited++
		return visited < 2
	})
	if visited != 2 {
		t.Errorf("visited = %d, want the walk to stop at 2", visited)
	}
}

func TestCite(t *testing.T) {
	var o Outline
	n, _ := o.Add("", "Point")
	ref := Ref{DocID: "report", SectionID: 3, Excerpt: "68% said..."}

	if err := o.Cite(n.ID, ref); err != nil {
		t.Fatalf("Cite: %v", err)
	}
	if len(n.Grounding) != 1 {
		t.Fatalf("Grounding = %d, want 1", len(n.Grounding))
	}
	// Citing the same section twice is a normal thing to do by accident and
	// should not duplicate or error.
	if err := o.Cite(n.ID, ref); err != nil {
		t.Fatalf("second Cite: %v", err)
	}
	if len(n.Grounding) != 1 {
		t.Errorf("Grounding = %d after duplicate cite, want 1", len(n.Grounding))
	}
}

func TestCite_UnknownNode(t *testing.T) {
	var o Outline
	if err := o.Cite("n-nope", Ref{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestWords_UserAndGenerated(t *testing.T) {
	var o Outline
	n, _ := o.Add("", "One two") // 2 user words
	n.Notes = "three four five"  // 3 user words
	n.Draft = "a b c d"          // 4 generated words

	user, generated := o.Words()
	if user != 5 {
		t.Errorf("user words = %d, want 5", user)
	}
	if generated != 4 {
		t.Errorf("generated words = %d, want 4", generated)
	}
}

// An edited draft is neither purely the model's nor purely the author's, so it
// splits. Attributing the whole paragraph either way would misreport the thing
// the metric exists to show.
func TestWords_EditedDraftIsSplit(t *testing.T) {
	var o Outline
	n, _ := o.Add("", "T")
	n.Draft = "a b c d"
	n.DraftEdited = true

	user, generated := o.Words()
	if user != 1+2 { // title + half the draft
		t.Errorf("user words = %d, want 3", user)
	}
	if generated != 2 {
		t.Errorf("generated words = %d, want 2", generated)
	}
}

func TestLen(t *testing.T) {
	var o Outline
	if o.Len() != 0 {
		t.Errorf("empty Len = %d", o.Len())
	}
	a, _ := o.Add("", "A")
	_, _ = o.Add(a.ID, "A1")
	if o.Len() != 2 {
		t.Errorf("Len = %d, want 2", o.Len())
	}
}
