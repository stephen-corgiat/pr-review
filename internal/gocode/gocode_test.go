package gocode

import (
	"sort"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/fixturerepo"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
)

// fixture is the fixture PR's repo: the PR is C1..C2, and C0..C1 is an
// earlier commit that changes a signature.
func fixture(t *testing.T) *fixturerepo.Repo {
	t.Helper()
	r, err := fixturerepo.Build("../../testdata/dispatchly/repo", fixturerepo.CacheParent())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// buildSources builds gocode.FileSource for every .go file changed between
// base and head in r.
func buildSources(t *testing.T, r *gitsrc.Repo) []FileSource {
	t.Helper()
	files, err := r.NameStatus()
	if err != nil {
		t.Fatal(err)
	}
	var out []FileSource
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		dl, err := r.FullDiff(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		fs := FileSource{
			Path:       f.Path,
			ChangedOld: map[int]bool{},
			ChangedNew: map[int]bool{},
		}
		for _, l := range dl {
			switch l.Kind {
			case gitsrc.LineAdded:
				fs.ChangedNew[l.NewNo] = true
			case gitsrc.LineRemoved:
				fs.ChangedOld[l.OldNo] = true
			}
		}
		if old, ok := r.Show(r.Base, f.Path); ok {
			fs.OldContent, fs.OldExists = old, true
		}
		if nw, ok := r.Show(r.Head, f.Path); ok {
			fs.NewContent, fs.NewExists = nw, true
		}
		out = append(out, fs)
	}
	return out
}

func TestFixturePRTouchedMethodsAndEdges(t *testing.T) {
	fr := fixture(t)
	r := gitsrc.OpenRepo(fr.Dir, fr.C1, fr.C2)
	sources := buildSources(t, r)
	modulePath := r.GoModPath()
	if modulePath != "github.com/acme/dispatchly" {
		t.Fatalf("unexpected module path %q", modulePath)
	}

	an := Analyze(sources, modulePath)
	if len(an.ParseNotes) != 0 {
		t.Fatalf("unexpected parse notes: %+v", an.ParseNotes)
	}

	wantMethods := []string{
		"internal/bounce/webhooks/beacon.go::NewBeacon",
		"internal/bounce/webhooks/beacon.go::(*Beacon).ProcessHandshake",
		"internal/bounce/webhooks/beacon.go::(*Beacon).ProcessBounce",
		"internal/bounce/webhooks/beacon.go::(*Beacon).verifyAuth",
		"internal/bounce/webhooks/beacon.go::secretsEqual",
		"internal/bounce/webhooks/beacon.go::parseBeaconEvents",
		"internal/bounce/webhooks/beacon.go::mapBeaconStatus",
		"cmd/bounce.go::(*App).BounceWebhook",
		"cmd/init.go::initConstConfig",
		"cmd/init.go::initBounceManager",
		"cmd/settings.go::(*App).GetSettings",
		"cmd/settings.go::(*App).UpdateSettings",
		"internal/bounce/bounce.go::New",
		"internal/migrations/v1.3.0.go::V1_3_0",
	}
	var gotKeys []string
	for _, m := range an.Methods {
		gotKeys = append(gotKeys, m.Key)
	}
	sort.Strings(gotKeys)
	sortedWant := append([]string(nil), wantMethods...)
	sort.Strings(sortedWant)
	if len(gotKeys) != len(sortedWant) {
		t.Fatalf("got %d Touched Methods, want %d\ngot:  %v\nwant: %v", len(gotKeys), len(sortedWant), gotKeys, sortedWant)
	}
	for i := range gotKeys {
		if gotKeys[i] != sortedWant[i] {
			t.Errorf("method[%d] = %q, want %q", i, gotKeys[i], sortedWant[i])
		}
	}

	// Methods in the two new files are "added"; the rest are "changed".
	for _, m := range an.Methods {
		wantStatus := Changed
		if strings.Contains(m.Path, "beacon.go") || strings.Contains(m.Path, "v1.3.0.go") {
			wantStatus = Added
		}
		if m.Status != wantStatus {
			t.Errorf("%s: status = %v, want %v", m.Key, m.Status, wantStatus)
		}
	}

	wantEdges := [][2]string{
		{"cmd/bounce.go::(*App).BounceWebhook", "internal/bounce/webhooks/beacon.go::(*Beacon).ProcessHandshake"},
		{"cmd/bounce.go::(*App).BounceWebhook", "internal/bounce/webhooks/beacon.go::(*Beacon).ProcessBounce"},
		{"cmd/init.go::initBounceManager", "internal/bounce/bounce.go::New"},
		{"internal/bounce/bounce.go::New", "internal/bounce/webhooks/beacon.go::NewBeacon"},
		{"internal/bounce/webhooks/beacon.go::(*Beacon).ProcessBounce", "internal/bounce/webhooks/beacon.go::(*Beacon).verifyAuth"},
		{"internal/bounce/webhooks/beacon.go::(*Beacon).ProcessBounce", "internal/bounce/webhooks/beacon.go::parseBeaconEvents"},
		{"internal/bounce/webhooks/beacon.go::(*Beacon).ProcessBounce", "internal/bounce/webhooks/beacon.go::mapBeaconStatus"},
		{"internal/bounce/webhooks/beacon.go::(*Beacon).verifyAuth", "internal/bounce/webhooks/beacon.go::secretsEqual"},
	}
	var gotEdges []string
	edgeSet := map[string]bool{}
	for _, e := range an.Edges {
		s := e.Caller + " -> " + e.Callee
		gotEdges = append(gotEdges, s)
		edgeSet[s] = true
	}
	if len(an.Edges) != len(wantEdges) {
		t.Errorf("got %d edges, want %d\ngot:\n%s", len(an.Edges), len(wantEdges), strings.Join(gotEdges, "\n"))
	}
	for _, w := range wantEdges {
		s := w[0] + " -> " + w[1]
		if !edgeSet[s] {
			t.Errorf("missing edge %s", s)
		}
	}

	// Exported-API changes.
	wantAdded := map[string]bool{
		"webhooks.NewBeacon": true, "(*webhooks.Beacon).ProcessHandshake": true,
		"(*webhooks.Beacon).ProcessBounce": true, "webhooks.Beacon": true,
	}
	wantFieldsAdded := map[string]bool{
		"bounce.Opt.BeaconEnabled": true, "bounce.Opt.BeaconSecret": true, "bounce.Opt.BeaconSecretHeader": true,
		"bounce.Manager.Beacon": true, "models.Settings.BounceBeacon": true,
	}
	gotAdded := map[string]bool{}
	gotFieldsAdded := map[string]bool{}
	for _, c := range an.ExportedChanges {
		switch c.Kind {
		case "func_added", "type_added":
			gotAdded[c.Name] = true
		case "field_added":
			gotFieldsAdded[c.Name] = true
		}
		if strings.HasPrefix(c.Path, "cmd/") {
			t.Errorf("cmd/*.go should be skipped (package main), got change in %s", c.Path)
		}
	}
	for k := range wantAdded {
		if !gotAdded[k] {
			t.Errorf("missing exported-added change %q; got %v", k, gotAdded)
		}
	}
	for k := range wantFieldsAdded {
		if !gotFieldsAdded[k] {
			t.Errorf("missing exported field-added change %q; got %v", k, gotFieldsAdded)
		}
	}
}

func TestSignatureChangedRepoMode(t *testing.T) {
	fr := fixture(t)
	// C0..C1 changes UpdateContactWithLists's signature.
	sources := buildSources(t, gitsrc.OpenRepo(fr.Dir, fr.C0, fr.C1))
	an := Analyze(sources, "github.com/acme/dispatchly")
	found := false
	for _, c := range an.ExportedChanges {
		if c.Kind == "func_sig_changed" && strings.Contains(c.Name, "UpdateContactWithLists") {
			found = true
			t.Logf("found: %s %s", c.Name, c.Detail)
		}
	}
	if !found {
		t.Errorf("expected (*core.Core).UpdateContactWithLists to be reported as signature changed")
	}
}
