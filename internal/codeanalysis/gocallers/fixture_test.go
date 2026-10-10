package gocallers_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/gocallers"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/fixturerepo"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

const (
	fixtures = "../../../testdata/dispatchly"
	beacon   = "internal/bounce/webhooks/beacon.go"
)

// callersOf lists the functions that call a method, once per call site.
func callersOf(res gocallers.Result, key string) []string {
	var out []string
	for _, c := range res[key].Callers {
		out = append(out, c.Func)
	}
	return out
}

func distinct(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// The analysis on the fixture PR (dispatchly c1..c2): every Go Touched
// Method gets its exact callers, found by type-checking the head.
func TestFixturePRCallers(t *testing.T) {
	fr, err := fixturerepo.Build(fixtures+"/repo", fixturerepo.CacheParent())
	if err != nil {
		t.Fatal(err)
	}
	st, _, err := session.Build(gitsrc.OpenRepo(fr.Dir, fr.C1, fr.C2))
	if err != nil {
		t.Fatal(err)
	}
	ms := st.GoMethods()
	if len(ms) != 14 {
		t.Fatalf("%d Go Touched Methods, want 14", len(ms))
	}
	res := gocallers.Analyze(context.Background(), fr.Dir, ms)

	for key, want := range map[string]string{
		beacon + "::NewBeacon":                  "bounce.New",
		beacon + "::(*Beacon).ProcessHandshake": "BounceWebhook",
		beacon + "::(*Beacon).ProcessBounce":    "BounceWebhook",
		beacon + "::(*Beacon).verifyAuth":       "ProcessBounce",
		beacon + "::secretsEqual":               "verifyAuth",
		beacon + "::parseBeaconEvents":          "ProcessBounce",
		beacon + "::mapBeaconStatus":            "ProcessBounce",
		"cmd/init.go::initBounceManager":        "main",
	} {
		got := distinct(callersOf(res, key))
		if len(got) != 1 || !has(got, want) {
			t.Errorf("%s: callers %v, want just %q", key, got, want)
		}
		if u := res[key].Unavailable; u != "" {
			t.Errorf("%s: analysis unavailable: %s", key, u)
		}
	}
}

// The engine runs the analysis after open: cold, then from the cache.
func TestFixturePRCallersThroughTheEngine(t *testing.T) {
	cache := t.TempDir()
	open := func() (*engine.Engine, time.Duration) {
		eng, _, err := app.Fixture{FixturesDir: fixtures, CloneDir: fixturerepo.CacheParent(), Callers: engine.GoCallers, CacheDir: cache}.Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(eng.Close)
		start := time.Now()
		eng.Start()
		eng.WaitCallers(2 * time.Minute)
		if st := eng.State().CallersState; st != session.CallersOK {
			t.Fatalf("state %d %q", st, eng.State().CallersErr)
		}
		return eng, time.Since(start)
	}
	eng, cold := open()
	_, cached := open()
	fi, err := os.Stat(eng.CallersCachePath(eng.State().PR.HeadSHA))
	if err != nil {
		t.Fatal(err)
	}
	c, _, _ := eng.State().MethodCallers(beacon + "::NewBeacon")
	if len(c.Callers) != 1 || c.Callers[0].Func != "bounce.New" {
		t.Errorf("NewBeacon callers %+v", c)
	}
	t.Logf("cold %s, from the cache %s, cache file %d bytes", cold.Round(time.Millisecond), cached.Round(time.Microsecond), fi.Size())
}
