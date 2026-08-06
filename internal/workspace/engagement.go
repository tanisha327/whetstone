package workspace

import (
	"fmt"

	"github.com/tanisha327/whetstone/internal/provoke"
)

// Engagement is a running account of how much of the work is the author's.
//
// It exists because the drift it measures is invisible: nobody decides to stop
// thinking, they just accept one draft and then another. Showing the ratio
// while the work is in progress is a cheap check against that.
//
// The numbers are modest about what they can see — see Caveat.
type Engagement struct {
	SectionsTotal int
	SectionsRead  int

	ProvocationsTotal     int
	ProvocationsEngaged   int
	ProvocationsDismissed int
	ProvocationsOpen      int

	OutlineNodes int
	// UserWords counts titles, notes, and provocation responses: everything
	// the user typed. GeneratedWords counts draft prose. An edited draft is
	// split between the two.
	UserWords      int
	GeneratedWords int
}

// Engagement computes the current report.
func (w *Workspace) Engagement() Engagement {
	var e Engagement

	for _, d := range w.Documents {
		e.SectionsTotal += len(d.Sections)
		for _, s := range d.Sections {
			if w.IsRead(d.ID, s.ID) {
				e.SectionsRead++
			}
		}
	}

	for _, p := range w.Provocations {
		e.ProvocationsTotal++
		switch p.Status {
		case provoke.StatusEngaged:
			e.ProvocationsEngaged++
		case provoke.StatusDismissed:
			e.ProvocationsDismissed++
		default:
			e.ProvocationsOpen++
		}
		e.UserWords += wordCount(p.Response)
	}

	e.OutlineNodes = w.Outline.Len()
	user, generated := w.Outline.Words()
	e.UserWords += user
	e.GeneratedWords += generated

	return e
}

// AuthorshipFraction returns the share of words in the argument that the user
// wrote, in [0,1]. Returns 0 when nothing has been written either way.
func (e Engagement) AuthorshipFraction() float64 {
	total := e.UserWords + e.GeneratedWords
	if total == 0 {
		return 0
	}
	return float64(e.UserWords) / float64(total)
}

// Caveat is shown alongside the report. Stating the limits of the measurement
// is part of the measurement: a metric presented as more precise than it is
// becomes a target to game rather than a prompt to reflect.
const Caveat = "Counts opened sections and typed words. It cannot tell whether you read carefully or thought hard."

// Summary renders a one-line report for the status bar.
func (e Engagement) Summary() string {
	return fmt.Sprintf("read %d/%d · provocations %d open, %d resolved · yours %d%%",
		e.SectionsRead, e.SectionsTotal,
		e.ProvocationsOpen, e.ProvocationsEngaged+e.ProvocationsDismissed,
		int(e.AuthorshipFraction()*100+0.5))
}

func wordCount(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			inWord = false
		default:
			if !inWord {
				n++
				inWord = true
			}
		}
	}
	return n
}
