package treesitter

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/catest"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
)

// holocron #56 (merged): "Add Scrypted camera hotlink tiles (grid +
// filmstrip) and both-off gating", a stacked TypeScript/TSX PR. The diff
// base is the merge base of its base and head.
const (
	holocron56Base = "268554a6ee0ff3ca46984b13a4387844d7c09bc4"
	holocron56Head = "12d55670180e8d3930337b9cab27f84e2aaa02cf"
)

// holocron56 reads #56's changed files from a holocron checkout, read
// only (git show, git diff). It skips without one: $PR_REVIEW_HOLOCRON, or
// ~/Projects/holocron.
func holocron56(t *testing.T) []codeanalysis.FilePair {
	t.Helper()
	dir := os.Getenv("PR_REVIEW_HOLOCRON")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Projects", "holocron")
	}
	for _, sha := range []string{holocron56Base, holocron56Head} {
		if err := exec.Command("git", "-C", dir, "cat-file", "-e", sha+"^{commit}").Run(); err != nil {
			t.Skipf("no holocron checkout with #56's commits at %s (set PR_REVIEW_HOLOCRON)", dir)
		}
	}
	r := &gitsrc.Repo{Dir: dir, Base: holocron56Base, Head: holocron56Head}
	files, err := r.NameStatus()
	if err != nil {
		t.Fatal(err)
	}
	var out []codeanalysis.FilePair
	for _, f := range files {
		if !New().Reads(f.Path) {
			continue
		}
		lines, err := r.FullDiff(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		fp := codeanalysis.FilePair{Path: f.Path, ChangedOld: map[int]bool{}, ChangedNew: map[int]bool{}}
		fp.Old, fp.OldExists = r.Show(r.Base, f.Path)
		fp.New, fp.NewExists = r.Show(r.Head, f.Path)
		for _, l := range lines {
			switch l.Kind {
			case gitsrc.LineAdded:
				fp.ChangedNew[l.NewNo] = true
			case gitsrc.LineRemoved:
				fp.ChangedOld[l.OldNo] = true
			}
		}
		out = append(out, fp)
	}
	return out
}

// 26 Touched Methods and 14 edges, checked by hand against the diff.
// Files that only change types, comments or test bodies (describe/it
// callbacks) have none.
func TestHolocron56(t *testing.T) {
	r := codeanalysis.Run(holocron56(t), New())
	if len(r.ParseNotes) > 0 {
		t.Errorf("parse notes: %+v", r.ParseNotes)
	}
	catest.Methods(t, r, []string{
		"apps/api/src/app.module.ts::(AppModule).register changed new 41-56 decl 41 old 41-55 decl 41",
		"apps/api/src/ha/ha-config.ts::isCamerasConfigured added new 63-72 decl 68",
		"apps/api/src/ha/ha-config.ts::isHaConfigured changed new 55-61 decl 59 old 32-39 decl 37",
		"apps/api/src/ha/ha-config.ts::isHomeConfigured added new 74-84 decl 80",
		"apps/api/src/ha/ha-config.ts::loadHaConfig changed new 128-150 decl 128 old 50-64 decl 50",
		"apps/api/src/ha/ha-config.ts::parseCameras added new 88-117 decl 94",
		"apps/api/src/ha/ha-events.service.ts::(HaEventsService).onModuleInit changed new 37-54 decl 37 old 37-49 decl 37",
		"apps/api/src/ha/ha-proxy.service.spec.ts::make changed new 35-41 decl 35 old 33-38 decl 33",
		"apps/api/src/ha/ha-proxy.service.ts::(HaProxyService).cameraFields added new 23-37 decl 26",
		"apps/api/src/ha/ha-proxy.service.ts::(HaProxyService).haEnabled added new 39-44 decl 42",
		"apps/api/src/ha/ha-proxy.service.ts::(HaProxyService).snapshot changed new 46-84 decl 52 old 23-48 decl 28",
		"apps/e2e/src/tests/home-cameras.spec.ts::mockHotlink added new 33-43 decl 34",
		"apps/web/src/pages/TabletPage.tsx::TabletPage changed new 101-1065 decl 101 old 101-1056 decl 101",
		"apps/web/src/tablet/home/CameraFilmstrip.tsx::CameraFilmstrip added new 6-38 decl 14",
		"apps/web/src/tablet/home/CameraFilmstrip.tsx::FilmstripThumb added new 50-166 decl 50",
		"apps/web/src/tablet/home/CameraTile.tsx::CameraTile added new 52-301 decl 60",
		"apps/web/src/tablet/home/CameraTile.tsx::ageLabel added new 32-36 decl 32",
		"apps/web/src/tablet/home/CameraTile.tsx::tintFor added new 22-30 decl 23",
		"apps/web/src/tablet/home/HomeDock.tsx::HomeDock changed new 6-139 decl 14 old 4-107 decl 12",
		"apps/web/src/tablet/home/HomeView.tsx::HomeView changed new 6-122 decl 16 old 4-54 decl 12",
		"apps/web/src/tablet/home/camera-status.ts::buildCameraSrc added new 115-134 decl 121",
		"apps/web/src/tablet/home/camera-status.ts::deriveCameraStatus added new 79-113 decl 93",
		"apps/web/src/tablet/home/camera-status.ts::initialCameraState added new 47-49 decl 47",
		"apps/web/src/tablet/home/camera-status.ts::reduceCameraState added new 56-71 decl 61",
		"apps/web/src/tablet/home/useCameraFrame.ts::useCameraFrame added new 29-94 decl 39",
		"apps/web/src/tablet/home/useHaSource.ts::useHaSource changed new 32-103 decl 38 old 19-87 decl 25",
	})
	catest.Edges(t, r, []string{
		"apps/api/src/app.module.ts::(AppModule).register -> apps/api/src/ha/ha-config.ts::isHomeConfigured",
		"apps/api/src/ha/ha-config.ts::isCamerasConfigured -> apps/api/src/ha/ha-config.ts::parseCameras",
		"apps/api/src/ha/ha-config.ts::isHomeConfigured -> apps/api/src/ha/ha-config.ts::isHaConfigured",
		"apps/api/src/ha/ha-config.ts::isHomeConfigured -> apps/api/src/ha/ha-config.ts::isCamerasConfigured",
		"apps/api/src/ha/ha-config.ts::loadHaConfig -> apps/api/src/ha/ha-config.ts::parseCameras",
		"apps/api/src/ha/ha-proxy.service.ts::(HaProxyService).snapshot -> apps/api/src/ha/ha-proxy.service.ts::(HaProxyService).cameraFields",
		"apps/web/src/pages/TabletPage.tsx::TabletPage -> apps/web/src/tablet/home/useHaSource.ts::useHaSource",
		"apps/web/src/tablet/home/CameraFilmstrip.tsx::FilmstripThumb -> apps/web/src/tablet/home/useCameraFrame.ts::useCameraFrame",
		"apps/web/src/tablet/home/CameraFilmstrip.tsx::FilmstripThumb -> apps/web/src/tablet/home/CameraTile.tsx::tintFor",
		"apps/web/src/tablet/home/CameraTile.tsx::CameraTile -> apps/web/src/tablet/home/useCameraFrame.ts::useCameraFrame",
		"apps/web/src/tablet/home/CameraTile.tsx::CameraTile -> apps/web/src/tablet/home/CameraTile.tsx::ageLabel",
		"apps/web/src/tablet/home/CameraTile.tsx::CameraTile -> apps/web/src/tablet/home/CameraTile.tsx::tintFor",
		"apps/web/src/tablet/home/useCameraFrame.ts::useCameraFrame -> apps/web/src/tablet/home/camera-status.ts::buildCameraSrc",
		"apps/web/src/tablet/home/useCameraFrame.ts::useCameraFrame -> apps/web/src/tablet/home/camera-status.ts::deriveCameraStatus",
	})
	// Known gap, expected: JSX isn't a call, so HomeView's <CameraTile />
	// and HomeDock's <CameraFilmstrip /> aren't edges.
	for _, e := range r.Edges {
		if strings.HasSuffix(e.Callee, "::CameraFilmstrip") || strings.HasSuffix(e.Callee, "::CameraTile") {
			t.Errorf("a JSX element counted as a call: %+v", e)
		}
	}
	if got, want := r.DeclLines["apps/web/src/tablet/home/camera-status.ts"], []int{11, 14, 20, 26, 33, 47, 51, 56, 73, 79, 115}; !reflect.DeepEqual(got, want) {
		t.Errorf("camera-status.ts decl stops = %v", got)
	}
}
