// Package workspace is the persisted session: documents, lens summaries,
// provocations, the outline, and the engagement record.
//
// Two rules differ from the usual config-loading pattern:
//
//   - A corrupt file is an error, not a reason to start empty. Substituting an
//     empty workspace and then saving over it would destroy the user's work.
//   - Saves are atomic (temp file plus rename), so a crash mid-write cannot
//     truncate the file.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/lens"
	"github.com/tanisha327/whetstone/internal/outline"
	"github.com/tanisha327/whetstone/internal/provoke"
)

// Version is the on-disk schema version. It is written to every file so a
// future reader can migrate rather than guess.
const Version = 1

// Workspace is one piece of work: the question, its sources, and the argument
// being built from them.
type Workspace struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	// Question is what the user is trying to answer. It is shown at all times
	// and injected into provocation context, so the critic argues about the
	// actual task rather than the text in the abstract.
	Question string `json:"question,omitempty"`

	Documents []*doc.Document `json:"documents,omitempty"`
	// ActiveLens is the lens ID currently applied.
	ActiveLens string `json:"activeLens,omitempty"`
	// Summaries is keyed by "<docID>#<sectionID>#<lensID>".
	Summaries map[string]lens.Summary `json:"summaries,omitempty"`
	// Read records which sections the user has actually opened, keyed
	// "<docID>#<sectionID>". This is the honest denominator of the engagement
	// report: it counts opening a section, which is the most this tool can
	// observe, and the report says so.
	Read map[string]bool `json:"read,omitempty"`

	Provocations []provoke.Provocation `json:"provocations,omitempty"`
	Outline      outline.Outline       `json:"outline"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// path is where this workspace was loaded from or will be saved to.
	// Unexported so it is not persisted into its own file.
	path string
}

// New returns an empty workspace bound to path.
func New(name, path string) *Workspace {
	now := time.Now()
	return &Workspace{
		Version:   Version,
		Name:      name,
		Summaries: map[string]lens.Summary{},
		Read:      map[string]bool{},
		CreatedAt: now,
		UpdatedAt: now,
		path:      path,
	}
}

// Path reports the file backing this workspace.
func (w *Workspace) Path() string { return w.path }

// ErrCorrupt reports an unreadable workspace file. The original file is left
// untouched and its location is in the message, so the user can inspect or
// recover it by hand.
type ErrCorrupt struct {
	Path string
	Err  error
}

func (e *ErrCorrupt) Error() string {
	return fmt.Sprintf("workspace %s is not readable: %v\n\n"+
		"The file has been left untouched. Inspect or move it, then retry.", e.Path, e.Err)
}

func (e *ErrCorrupt) Unwrap() error { return e.Err }

// Load reads a workspace. A missing file is not an error — that is how a
// session starts. An unparseable file returns *ErrCorrupt and no workspace.
func Load(path string) (*Workspace, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(defaultName(path), path), nil
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: reading %s: %w", path, err)
	}

	var w Workspace
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, &ErrCorrupt{Path: path, Err: err}
	}
	if w.Version > Version {
		return nil, &ErrCorrupt{
			Path: path,
			Err:  fmt.Errorf("written by a newer version (schema %d, this build reads %d)", w.Version, Version),
		}
	}
	if w.Summaries == nil {
		w.Summaries = map[string]lens.Summary{}
	}
	if w.Read == nil {
		w.Read = map[string]bool{}
	}
	w.path = path
	return &w, nil
}

// Save writes the workspace atomically: temp file, fsync, rename. A reader
// never sees a half-written file, and a crash mid-write cannot truncate it.
func (w *Workspace) Save() error {
	if w.path == "" {
		return errors.New("workspace: no path set")
	}
	w.UpdatedAt = time.Now()
	w.Version = Version

	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: encoding: %w", err)
	}

	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("workspace: creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".whetstone-*.tmp")
	if err != nil {
		return fmt.Errorf("workspace: creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	// From here on, any failure must not leave the temp file behind.
	defer os.Remove(tmpName)

	// Best-effort: Chmod is a no-op or unsupported on some Windows filesystems,
	// where the file is already user-scoped. Not worth failing a save over.
	_ = tmp.Chmod(0o600)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("workspace: writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("workspace: syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workspace: closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, w.path); err != nil {
		return fmt.Errorf("workspace: replacing %s: %w", w.path, err)
	}
	return nil
}

// --- Document and section access ---

// AddDocument appends a document, replacing any existing one with the same ID.
func (w *Workspace) AddDocument(d *doc.Document) {
	for i, existing := range w.Documents {
		if existing.ID == d.ID {
			w.Documents[i] = d
			return
		}
	}
	w.Documents = append(w.Documents, d)
}

// Document returns the document with the given ID.
func (w *Workspace) Document(id string) (*doc.Document, bool) {
	for _, d := range w.Documents {
		if d.ID == id {
			return d, true
		}
	}
	return nil, false
}

// SectionKey is the map key for a section within a workspace.
func SectionKey(docID string, sectionID int) string {
	return docID + "#" + strconv.Itoa(sectionID)
}

// summaryKey is the map key for one section viewed through one lens.
func summaryKey(docID string, sectionID int, lensID string) string {
	return SectionKey(docID, sectionID) + "#" + lensID
}

// MarkRead records that the user opened a section.
func (w *Workspace) MarkRead(docID string, sectionID int) {
	if w.Read == nil {
		w.Read = map[string]bool{}
	}
	w.Read[SectionKey(docID, sectionID)] = true
}

// IsRead reports whether a section has been opened.
func (w *Workspace) IsRead(docID string, sectionID int) bool {
	return w.Read[SectionKey(docID, sectionID)]
}

// PutSummary stores a lens summary.
func (w *Workspace) PutSummary(docID string, s lens.Summary) {
	if w.Summaries == nil {
		w.Summaries = map[string]lens.Summary{}
	}
	w.Summaries[summaryKey(docID, s.SectionID, s.LensID)] = s
}

// Summary returns a stored lens summary.
func (w *Workspace) Summary(docID string, sectionID int, lensID string) (lens.Summary, bool) {
	s, ok := w.Summaries[summaryKey(docID, sectionID, lensID)]
	return s, ok
}

// --- Provocations ---

// AddProvocations appends provocations, skipping duplicates on the same anchor
// so re-provoking a section does not bury the author in repeats.
func (w *Workspace) AddProvocations(ps []provoke.Provocation) int {
	added := 0
	for _, p := range ps {
		if w.hasSimilar(p) {
			continue
		}
		w.Provocations = append(w.Provocations, p)
		added++
	}
	return added
}

func (w *Workspace) hasSimilar(p provoke.Provocation) bool {
	norm := strings.ToLower(strings.Join(strings.Fields(p.Text), " "))
	for _, existing := range w.Provocations {
		if existing.AnchorKind != p.AnchorKind || existing.AnchorID != p.AnchorID {
			continue
		}
		if strings.ToLower(strings.Join(strings.Fields(existing.Text), " ")) == norm {
			return true
		}
	}
	return false
}

// Provocation returns a pointer to the stored provocation with the given ID so
// callers can resolve it in place.
func (w *Workspace) Provocation(id string) *provoke.Provocation {
	for i := range w.Provocations {
		if w.Provocations[i].ID == id {
			return &w.Provocations[i]
		}
	}
	return nil
}

// ProvocationsFor returns the provocations anchored to a target.
func (w *Workspace) ProvocationsFor(kind provoke.AnchorKind, anchorID string) []provoke.Provocation {
	var out []provoke.Provocation
	for _, p := range w.Provocations {
		if p.AnchorKind == kind && p.AnchorID == anchorID {
			out = append(out, p)
		}
	}
	return out
}

func defaultName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
