package dashboard

import (
	"context"
)

// The setup guide turns the server-verified readiness checklist into an
// ordered, opinionated walkthrough: for every prerequisite it shows the
// LIVE state (not static instructions), why it matters, and the exact
// command or page that fixes it. The whole point is that an operator can
// stand a PDP SP up by working top-to-bottom and never has to guess what
// "ready" means — the daemon verifies each step for them.
//
// It is built entirely on top of computeReadiness so the two views can
// never disagree: the overview shows the compact scorecard, this page is
// the same truth expanded into next-actions.

// guideAction is one remediation the operator can take for a step: either a
// copy-paste command or a link into the dashboard. Both are optional; a step
// that is already satisfied carries none.
type guideAction struct {
	Label string // human label for the button / row
	Cmd   string // copy-paste shell command (mutually exclusive with Href)
	Href  string // internal dashboard link
}

// guideStep is one prerequisite, enriched with teaching copy + remediation.
type guideStep struct {
	Num      int
	Key      string
	Title    string
	Why      string
	State    readyState
	Detail   string
	Critical bool
	Locked   bool // an earlier required step is not satisfied yet
	Actions  []guideAction
}

// Done reports whether the step is satisfied (ok state).
func (g guideStep) Done() bool { return g.State == readyOK }

// guideReport is the ordered walkthrough plus a single "do this next" hint.
type guideReport struct {
	Steps    []guideStep
	OK       int
	Total    int
	AllReady bool
	NextNum  int        // step number to do next; 0 when fully ready
	Next     *guideStep // convenience pointer to that step
}

// guideCopy is the static teaching content for a step, keyed by the readiness
// item Key. State/Detail are overlaid from the live readiness report.
type guideCopy struct {
	Title    string
	Why      string
	Critical bool
	// actionsFor returns remediation actions given the live state. Returning
	// nil (e.g. for a satisfied step) renders no call-to-action.
	actionsFor func(st readyState) []guideAction
}

// canonicalGuide is the ordered spine of a PDP SP bring-up. Order matters:
// each critical step gates the ones after it, so the walkthrough reads
// top-to-bottom.
func canonicalGuide() []struct {
	Key  string
	Copy guideCopy
} {
	return []struct {
		Key  string
		Copy guideCopy
	}{
		{"chain", guideCopy{
			Title:    "Connect the chain node",
			Why:      "curio-core reads and writes Filecoin through its own embedded Lantern node — no Lotus, no external RPC, no Glif API key. It starts with the daemon.",
			Critical: true,
			actionsFor: func(st readyState) []guideAction {
				if st == readyOK {
					return nil
				}
				return []guideAction{
					{Label: "Run diagnostics", Cmd: "curio-core doctor"},
				}
			},
		}},
		{"wallet", guideCopy{
			Title:    "Configure the PDP wallet",
			Why:      "The PDP pipeline signs proofs and settlement with a role='pdp' key held in the local eth_keys store. Create a fresh one or import a key you already control.",
			Critical: true,
			actionsFor: func(st readyState) []guideAction {
				if st == readyOK {
					return nil
				}
				return []guideAction{
					{Label: "Create a new PDP key", Cmd: "curio-core wallet new"},
					{Label: "Import an existing key", Cmd: "curio-core wallet import 0x<private-key>"},
				}
			},
		}},
		{"funded", guideCopy{
			Title:    "Fund the wallet for gas",
			Why:      "Every proof and settlement is an on-chain message. The PDP wallet needs a little FIL to pay gas, or proving stalls the moment a challenge lands.",
			Critical: true,
			actionsFor: func(st readyState) []guideAction {
				if st == readyOK {
					return nil
				}
				return []guideAction{
					{Label: "View wallet balance", Href: "/wallets"},
				}
			},
		}},
		{"stash", guideCopy{
			Title:    "Set the piece stash directory",
			Why:      "Client pieces are stream-committed straight into the stash directory — that file is the piece's permanent home, so it must be on durable storage with room to grow.",
			Critical: true,
			actionsFor: func(st readyState) []guideAction {
				if st == readyOK {
					return nil
				}
				return []guideAction{
					{Label: "Review storage paths", Href: "/storage"},
				}
			},
		}},
		{"datasets", guideCopy{
			Title: "Accept your first dataset",
			Why:   "A dataset is a client's proof set. You begin earning on a payment rail the moment one goes active — until then the SP is idle but healthy.",
			actionsFor: func(st readyState) []guideAction {
				if st == readyOK {
					return nil
				}
				return []guideAction{
					{Label: "Upload a piece", Href: "/upload"},
					{Label: "View datasets", Href: "/datasets"},
				}
			},
		}},
		{"proving", guideCopy{
			Title: "Keep proofs healthy",
			Why:   "PDP demands a proof every challenge window. A missed proof faults the dataset and pauses payment, so watch prove tasks and the mpool for stuck sends.",
			actionsFor: func(st readyState) []guideAction {
				if st == readyOK {
					return nil
				}
				return []guideAction{
					{Label: "View prove tasks", Href: "/tasks"},
					{Label: "Check the mpool", Href: "/messages"},
				}
			},
		}},
	}
}

// computeGuide expands the live readiness report into the ordered setup
// walkthrough. It never invents state: every step's State/Detail come from
// computeReadiness. Steps whose readiness item is absent because a
// prerequisite is unmet (e.g. "funded" before a wallet exists) render as
// locked rather than as a false failure.
func (s *Server) computeGuide(ctx context.Context, ov overviewData) guideReport {
	rep := s.computeReadiness(ctx, ov)

	byKey := make(map[string]readinessItem, len(rep.Items))
	for _, it := range rep.Items {
		byKey[it.Key] = it
	}

	var out guideReport
	num := 0
	priorCriticalUnmet := false
	for _, c := range canonicalGuide() {
		num++
		step := guideStep{
			Num:      num,
			Key:      c.Key,
			Title:    c.Copy.Title,
			Why:      c.Copy.Why,
			Critical: c.Copy.Critical,
		}

		if it, ok := byKey[c.Key]; ok {
			step.State = it.State
			step.Detail = it.Detail
			// A canonical step marked critical stays critical even if the
			// readiness item didn't flag it (defensive against drift).
			if it.Critical {
				step.Critical = true
			}
		} else {
			// Readiness omitted this item because its prerequisite isn't met.
			step.State = readyUnknown
			step.Locked = true
			step.Detail = "waiting on an earlier step"
		}

		// A critical step is locked (greyed, no premature CTA) when an
		// earlier critical step is still unmet — keeps the walkthrough
		// strictly sequential.
		if step.Critical && priorCriticalUnmet && step.State != readyOK {
			step.Locked = true
		}

		if !step.Locked && c.Copy.actionsFor != nil {
			step.Actions = c.Copy.actionsFor(step.State)
		}

		if step.State == readyOK {
			out.OK++
		} else if step.Critical {
			// mark that later critical steps depend on this one
			priorCriticalUnmet = true
			if out.NextNum == 0 {
				out.NextNum = step.Num
			}
		} else if out.NextNum == 0 {
			// first non-critical gap only becomes "next" if nothing critical
			// is outstanding
			out.NextNum = step.Num
		}

		out.Steps = append(out.Steps, step)
	}

	out.Total = len(out.Steps)
	out.AllReady = rep.AllReady
	if out.NextNum > 0 && out.NextNum <= len(out.Steps) {
		out.Next = &out.Steps[out.NextNum-1]
	}
	return out
}
