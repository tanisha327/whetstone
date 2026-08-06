// Package outline is the author's argument tree — the one thing here a model
// never writes.
//
// Drafts are generated from the outline, so the finished document has the shape
// the author built by hand. That ordering is the point: prose follows judgement
// rather than the reverse.
//
// Nodes therefore have no Generate method. Draft text lives on a node, but only
// an explicit user action puts it there.
package outline

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Ref cites a passage of a source document from an outline node. Grounding is
// what keeps a generated draft tied to the material.
type Ref struct {
	DocID     string `json:"docId"`
	SectionID int    `json:"sectionId"`
	// Excerpt is the quoted text, captured at citation time so the reference
	// survives a re-parse of the document.
	Excerpt string `json:"excerpt"`
}

// Node is one point in the argument.
type Node struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Notes is the user's own reasoning about this point, in their words.
	// It is the primary input to a generated draft and counts as authorship.
	Notes string `json:"notes,omitempty"`
	// Grounding is the evidence cited for this point.
	Grounding []Ref `json:"grounding,omitempty"`
	// Draft is generated prose for this node, if any.
	Draft string `json:"draft,omitempty"`
	// DraftEdited records that the user has since edited the generated text,
	// which reclassifies it as co-authored in the engagement metrics.
	DraftEdited bool    `json:"draftEdited,omitempty"`
	Children    []*Node `json:"children,omitempty"`
}

// WordCount returns the words the user personally wrote on this node.
func (n *Node) WordCount() int {
	return len(strings.Fields(n.Title)) + len(strings.Fields(n.Notes))
}

// draftWordCount returns the words generated for this node.
func (n *Node) draftWordCount() int { return len(strings.Fields(n.Draft)) }

// Outline is a forest of top-level points.
type Outline struct {
	Nodes []*Node `json:"nodes,omitempty"`
}

// Errors returned by mutating operations.
var (
	ErrNotFound   = errors.New("outline: node not found")
	ErrEmptyTitle = errors.New("outline: node title is empty")
)

// Add appends a node under parentID, or at the top level when parentID is "".
// It returns the new node.
func (o *Outline) Add(parentID, title string) (*Node, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, ErrEmptyTitle
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	n := &Node{ID: id, Title: title}

	if parentID == "" {
		o.Nodes = append(o.Nodes, n)
		return n, nil
	}
	parent := o.Find(parentID)
	if parent == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, parentID)
	}
	parent.Children = append(parent.Children, n)
	return n, nil
}

// Find returns the node with the given ID, or nil.
func (o *Outline) Find(id string) *Node {
	var found *Node
	o.walk(func(n *Node, _ int) bool {
		if n.ID == id {
			found = n
			return false
		}
		return true
	})
	return found
}

// Remove deletes a node and its subtree.
func (o *Outline) Remove(id string) error {
	if id == "" {
		return ErrNotFound
	}
	if removed := removeFrom(&o.Nodes, id); removed {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

func removeFrom(list *[]*Node, id string) bool {
	for i, n := range *list {
		if n.ID == id {
			*list = append((*list)[:i], (*list)[i+1:]...)
			return true
		}
		if removeFrom(&n.Children, id) {
			return true
		}
	}
	return false
}

// walk visits every node depth-first, passing its zero-based depth. Returning
// false from fn stops the traversal.
func (o *Outline) walk(fn func(n *Node, depth int) bool) {
	var visit func(nodes []*Node, depth int) bool
	visit = func(nodes []*Node, depth int) bool {
		for _, n := range nodes {
			if !fn(n, depth) {
				return false
			}
			if !visit(n.Children, depth+1) {
				return false
			}
		}
		return true
	}
	visit(o.Nodes, 0)
}

// Flat is a node paired with its depth, for rendering.
type Flat struct {
	Node  *Node
	Depth int
}

// Flatten returns the outline in display order.
func (o *Outline) Flatten() []Flat {
	var out []Flat
	o.walk(func(n *Node, d int) bool {
		out = append(out, Flat{Node: n, Depth: d})
		return true
	})
	return out
}

// Len returns the total node count.
func (o *Outline) Len() int {
	n := 0
	o.walk(func(*Node, int) bool { n++; return true })
	return n
}

// Cite attaches a grounding reference to a node.
func (o *Outline) Cite(nodeID string, ref Ref) error {
	n := o.Find(nodeID)
	if n == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, nodeID)
	}
	for _, existing := range n.Grounding {
		if existing.DocID == ref.DocID && existing.SectionID == ref.SectionID {
			return nil // already cited; citing twice is not an error
		}
	}
	n.Grounding = append(n.Grounding, ref)
	return nil
}

// Words returns user-authored and generated word totals across the outline.
func (o *Outline) Words() (user, generated int) {
	o.walk(func(n *Node, _ int) bool {
		user += n.WordCount()
		if n.DraftEdited {
			// Edited drafts are co-authored; split the difference rather than
			// claiming the whole paragraph for either side.
			half := n.draftWordCount() / 2
			user += half
			generated += n.draftWordCount() - half
		} else {
			generated += n.draftWordCount()
		}
		return true
	})
	return user, generated
}

func newID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("outline: generating id: %w", err)
	}
	return "n-" + hex.EncodeToString(b[:]), nil
}
