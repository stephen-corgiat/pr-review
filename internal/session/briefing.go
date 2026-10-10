package session

import (
	"regexp"
	"sort"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
)

// buildBriefing assembles the computed parts. The agent-written parts land
// later (BriefingArrived), and Ready stays false until then.
func buildBriefing(s *Session, an *codeanalysis.Result) *Briefing {
	b := &Briefing{Methods: map[string]*Method{}}
	for k, m := range s.Methods {
		b.Methods[k] = m
	}

	// Computed Contract Changes: exported-API changes and schema files.
	byKind := map[string][]codeanalysis.ExportedChange{}
	for _, c := range an.ExportedChanges {
		byKind[c.Kind] = append(byKind[c.Kind], c)
	}
	addExp := func(title string, cs []codeanalysis.ExportedChange) {
		if len(cs) == 0 {
			return
		}
		var names []string
		for _, c := range cs {
			names = append(names, c.Name)
		}
		b.ContractChanges = append(b.ContractChanges, ContractChange{
			Category: CategoryExported, Computed: true,
			Title:  title,
			Detail: strings.Join(names, ", "),
		})
	}
	addExp("Added", byKind["func_added"])
	addExp("Removed", byKind["func_removed"])
	if cs := byKind["func_sig_changed"]; len(cs) > 0 {
		for _, c := range cs {
			b.ContractChanges = append(b.ContractChanges, ContractChange{
				Category: CategoryExported, Computed: true,
				Title: "Signature changed: " + c.Name, Detail: c.Detail,
				Anchors: []ContractChangeAnchor{{Path: c.Path}},
			})
		}
	}
	addExp("Types added", byKind["type_added"])
	addExp("Types removed", byKind["type_removed"])
	addExp("Fields added", byKind["field_added"])
	addExp("Fields removed", byKind["field_removed"])

	var schemaFiles []string
	for p := range s.Files {
		if schemaFile(p) {
			schemaFiles = append(schemaFiles, p)
		}
	}
	sort.Strings(schemaFiles)
	if len(schemaFiles) > 0 {
		b.ContractChanges = append(b.ContractChanges, ContractChange{
			Category: CategorySchema, Computed: true,
			Title:  "Schema files touched",
			Detail: strings.Join(schemaFiles, ", "),
		})
	}

	// Reading order defaults to git order until the agent parts land.
	for _, p := range s.FileGitOrder {
		b.ReadingOrder = append(b.ReadingOrder, ReadingStep{Path: p})
	}

	return b
}

// schemaFile is the "schema file" rule: a migrations/migrate directory, or
// a .sql extension.
var schemaDirRe = regexp.MustCompile(`(?i)(^|/)(migrations?|migrate)/`)

func schemaFile(p string) bool {
	return schemaDirRe.MatchString(p) || strings.HasSuffix(strings.ToLower(p), ".sql")
}

// applyBriefing attaches the agent-written parts and re-sorts the
// navigator into reading order. Files the agent left out keep their place
// after the ones it ordered; paths it made up are dropped.
func (s *Session) applyBriefing(n BriefingNotes) {
	b := s.Briefing
	if b == nil {
		return
	}
	// A newer Briefing (after Update) replaces the agent-written parts.
	var computed []ContractChange
	for _, c := range b.ContractChanges {
		if c.Computed {
			computed = append(computed, c)
		}
	}
	b.ContractChanges = computed
	for _, m := range b.Methods {
		m.Does, m.Changed = "", ""
	}
	b.TLDR = n.TLDR
	var order []ReadingStep
	var paths []string
	seen := map[string]bool{}
	for _, r := range n.ReadingOrder {
		if s.Files[r.Path] == nil || seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		order = append(order, r)
		paths = append(paths, r.Path)
	}
	for _, p := range s.FileGitOrder {
		if !seen[p] {
			order = append(order, ReadingStep{Path: p})
			paths = append(paths, p)
		}
	}
	b.ReadingOrder = order
	s.ReadingOrder = paths
	for k, mn := range n.Methods {
		if m, ok := b.Methods[k]; ok {
			m.Does = mn.Does
			m.Changed = mn.Changed
		}
	}
	b.ContractChanges = append(b.ContractChanges, n.ContractChanges...)
	b.Ready = true
}

// FileOrder is the navigator's current file order: reading order once ready,
// git order until then.
func (s *Session) FileOrder() []string {
	if s.Briefing != nil && s.Briefing.Ready && len(s.ReadingOrder) > 0 {
		return s.ReadingOrder
	}
	return s.FileGitOrder
}
