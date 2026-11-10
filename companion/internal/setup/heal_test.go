package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

func (w *world) heal() HealResult {
	w.t.Helper()
	res, err := SelfHeal(context.Background(), w.deps, w.game)
	if err != nil {
		w.t.Fatalf("SelfHeal: %v", err)
	}
	return res
}

func TestHealReplacesAnAddonThatDiffersByContent(t *testing.T) {
	w := newWorld(t)
	w.preinstall(2, 3, "9.9.9")

	// Same files: nothing to do.
	if res := w.heal(); res.Changed() {
		t.Fatalf("identical addon was touched: %+v", res)
	}

	// Same TOC version, different Lua: a content hash, not the version, decides.
	core := filepath.Join(layout.AddonsDir(w.game), "Ompcraft", "Core", "Init.lua")
	if err := os.WriteFile(core, []byte("-- a stale build\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := w.heal()
	if !res.AddonReplaced || res.PluginRefreshed {
		t.Fatalf("res = %+v", res)
	}
	if b, _ := os.ReadFile(core); string(b) != "-- addon\n" {
		t.Errorf("addon not replaced: %q", b)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "self-heal: the installed addon differed") {
		t.Errorf("notes = %v", res.Notes)
	}

	// An extra file counts as a difference too.
	extra := filepath.Join(layout.AddonsDir(w.game), "Ompcraft", "Extra.lua")
	_ = os.WriteFile(extra, []byte("x"), 0o644)
	if res := w.heal(); !res.AddonReplaced || exists(extra) {
		t.Errorf("extra file survived: %+v", res)
	}
	if res := w.heal(); res.Changed() {
		t.Error("healing twice must be a no-op the second time")
	}
}

func TestHealIgnoresAMissingAddon(t *testing.T) {
	w := newWorld(t) // nothing installed
	if res := w.heal(); res.Changed() {
		t.Fatalf("res = %+v", res)
	}
	if exists(filepath.Join(layout.AddonsDir(w.game), "Ompcraft")) {
		t.Error("self-heal must not install the addon when the user has not")
	}
	// No game folder known at all.
	if res, err := SelfHeal(context.Background(), w.deps, ""); err != nil || res.Changed() {
		t.Errorf("%+v %v", res, err)
	}
}

func TestHealRefreshesThePluginCopyAndReloads(t *testing.T) {
	w := newWorld(t)
	w.preinstall(2, 3, "9.9.9")
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	w.reset()

	if res := w.heal(); res.Changed() {
		t.Fatalf("an up-to-date plugin copy was touched: %+v", res)
	}
	if w.sys.called("tern plugin reload ompcraft") {
		t.Fatal("no reload without a change")
	}

	// A newer binary carries a newer plugin.
	w.deps.Plugin = pluginFS("-- plugin v2\n")
	res := w.heal()
	if !res.PluginRefreshed || res.AddonReplaced {
		t.Fatalf("res = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(w.env.PluginDir(), "window.luau")); string(b) != "-- plugin v2\n" {
		t.Errorf("plugin copy = %q", b)
	}
	if !w.sys.called("tern plugin reload ompcraft") {
		t.Errorf("commands: %v", w.sys.calls)
	}
	if len(res.Notes) != 1 || !strings.HasSuffix(res.Notes[0], "and reloaded it") {
		t.Errorf("notes = %v", res.Notes)
	}
}

func TestHealLeavesADevLinkAlone(t *testing.T) {
	w := newWorld(t)
	w.preinstall(2, 3, "9.9.9")
	if err := w.setup(Options{}); err != nil {
		t.Fatal(err)
	}
	// The user now links their checkout; the data-dir copy is stale.
	dev := "/home/user/dev/ompcraft/tern-plugin"
	if err := os.WriteFile(filepath.Join(w.sys.plugDir, "ompcraft.path"), []byte(dev+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.deps.Plugin = pluginFS("-- plugin v2\n")
	w.reset()
	res := w.heal()
	if res.PluginRefreshed {
		t.Fatalf("a dev link must be left alone: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(w.env.PluginDir(), "window.luau")); string(b) != "-- plugin\n" {
		t.Errorf("the copy was rewritten: %q", b)
	}
	for _, c := range w.sys.calls {
		if strings.Contains(c, "reload") || strings.Contains(c, "link ") {
			t.Errorf("tern was driven: %s", c)
		}
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "linked from "+dev) {
		t.Errorf("notes = %v", res.Notes)
	}
}

func TestHealPluginWithoutTernOrCopy(t *testing.T) {
	w := newWorld(t)
	w.preinstall(2, 3, "9.9.9")
	// No data-dir copy: nothing to heal, tern is never asked.
	if res := w.heal(); res.Changed() || len(w.sys.calls) != 0 {
		t.Fatalf("res=%+v calls=%v", res, w.sys.calls)
	}
	// A copy exists but tern is gone: rewrite the copy, no commands.
	if err := os.MkdirAll(w.env.PluginDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.env.PluginDir(), "window.luau"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.sys.haveTern = false
	res := w.heal()
	if !res.PluginRefreshed || len(w.sys.calls) != 0 {
		t.Fatalf("res=%+v calls=%v", res, w.sys.calls)
	}
}

// oldHelperTOCs rewrites every helper TOC the way an earlier release did.
func (w *world) oldHelperTOCs(slots int) {
	w.t.Helper()
	a := layout.AddonsDir(w.game)
	for i := range slots {
		old := "## Interface: 16001\n## Title: |cff808080Ompcraft link slot|r\n## Group: Ompcraft\n## LoadOnDemand: 1\ndata.lua\n"
		if err := os.WriteFile(filepath.Join(a, layout.SlotName(i), layout.SlotName(i)+".toc"), []byte(old), 0o644); err != nil {
			w.t.Fatal(err)
		}
	}
	sig := "## Interface: 16001\n## Group: Ompcraft\n## X-Ompcraft-Slots: 3\n## X-Ompcraft-Signals: 4\n"
	if err := os.WriteFile(filepath.Join(a, "OmpcraftSignals", "OmpcraftSignals.toc"), []byte(sig), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func TestSetupRewritesOutdatedHelperTOCsWhileTheGameRuns(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.oldHelperTOCs(3)
	w.closeAfter = 1000 // WoW stays running: no waiting, TOCs are rewritten in place
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	out := w.output()
	if !strings.Contains(out, "✓ Updated the addon 9.9.9 (type /reload in game)\n• Restart WoW once to tidy the AddOns list (its helpers move into a collapsible \"Ompcraft link helpers\" section)\n") {
		t.Fatalf("output:\n%s", out)
	}
	if w.sleeps != 0 {
		t.Errorf("setup waited %d times", w.sleeps)
	}
	a := layout.AddonsDir(w.game)
	for i := range 3 {
		if b, _ := os.ReadFile(filepath.Join(a, layout.SlotName(i), layout.SlotName(i)+".toc")); string(b) != layout.SlotTOC(i) {
			t.Errorf("slot %d toc not rewritten:\n%s", i, b)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(a, "OmpcraftSignals", "OmpcraftSignals.toc")); string(b) != layout.SignalsTOC(3, 4) {
		t.Errorf("signals toc:\n%s", b)
	}
	// Nothing left to tidy: the next run says nothing about restarting.
	w.reset()
	w.closeAfter = 1000
	if err := w.setup(Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.output(), "Restart WoW once") {
		t.Errorf("repeat run:\n%s", w.output())
	}
}

func TestHealRewritesOutdatedHelperTOCs(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.oldHelperTOCs(3)
	res := w.heal()
	if res.HelpersRewritten != 4 || res.Changed() { // 3 slots + signals; TOCs alone are not an "update"
		t.Fatalf("res = %+v", res)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "rewrote 4 outdated helper TOC file(s)") {
		t.Errorf("notes = %v", res.Notes)
	}
	if res := w.heal(); res.HelpersRewritten != 0 {
		t.Errorf("second heal: %+v", res)
	}
}

func TestDoctorReportsOutdatedHelperTOCs(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.oldHelperTOCs(3)
	err := w.doctor()
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v", err)
	}
	out := w.output()
	if !strings.Contains(out, "✗ Link helpers: 3/3 slots, 4/4 doorbells present; 4 helper TOC file(s) outdated\n") {
		t.Errorf("output:\n%s", out)
	}
	if !strings.Contains(out, "Fix: run `ompcraft setup` (it updates them even while the game runs), then restart WoW once to tidy the AddOns list\n") {
		t.Errorf("fix line:\n%s", out)
	}
}

func TestDoctorShowsUpdateInfo(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	_ = w.setup(Options{})
	w.reset()
	_ = w.doctor()
	if !strings.Contains(w.output(), "• Updates: source build; auto-update is off\n") && !strings.Contains(w.output(), "✓ Updates: source build; auto-update is off\n") {
		t.Errorf("output:\n%s", w.output())
	}
}
