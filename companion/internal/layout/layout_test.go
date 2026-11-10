package layout

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSilentWAV(t *testing.T) {
	w := SilentWAV()
	if len(w) != 44+400 {
		t.Fatalf("len = %d, want 444 (44-byte header + 400 samples = 50 ms at 8 kHz)", len(w))
	}
	le := binary.LittleEndian
	if string(w[0:4]) != "RIFF" || string(w[8:16]) != "WAVEfmt " || string(w[36:40]) != "data" {
		t.Fatalf("bad chunk ids: %q %q %q", w[0:4], w[8:16], w[36:40])
	}
	if got := le.Uint32(w[4:]); int(got) != len(w)-8 {
		t.Errorf("RIFF size = %d, want %d", got, len(w)-8)
	}
	if le.Uint32(w[16:]) != 16 || le.Uint16(w[20:]) != 1 {
		t.Error("not a PCM fmt chunk")
	}
	if le.Uint16(w[22:]) != 1 || le.Uint32(w[24:]) != 8000 || le.Uint32(w[28:]) != 8000 ||
		le.Uint16(w[32:]) != 1 || le.Uint16(w[34:]) != 8 {
		t.Error("expected 8 kHz mono 8-bit")
	}
	if le.Uint32(w[40:]) != 400 {
		t.Errorf("data size = %d", le.Uint32(w[40:]))
	}
	if !bytes.Equal(w[44:], bytes.Repeat([]byte{0x80}, 400)) {
		t.Error("8-bit PCM silence is 0x80")
	}
}

func TestNames(t *testing.T) {
	if SlotName(7) != "OmpcraftS007" || DoorbellName(7) != "0007.wav" {
		t.Fatal("name formats")
	}
	for name, want := range map[string]bool{
		"OmpcraftS000": true, "OmpcraftS299": true,
		"OmpcraftS00": false, "OmpcraftS0000": false, "OmpcraftSabc": false,
		"OmpcraftSignals": false, "Ompcraft": false, "xOmpcraftS000": false, "OmpcraftS000.bak": false,
	} {
		if IsSlotName(name) != want {
			t.Errorf("IsSlotName(%q) = %v", name, !want)
		}
	}
	if i, ok := SlotIndex("OmpcraftS042"); !ok || i != 42 {
		t.Error("SlotIndex")
	}
	if IsDoorbellName("ctl_present.wav") || !IsDoorbellName("1999.wav") || IsDoorbellName("199.wav") || IsDoorbellName("0001.wav.tmp") {
		t.Error("IsDoorbellName")
	}
}

func TestSignalsTOC(t *testing.T) {
	want := "## Interface: 16001\n" +
		"## Title: |cff808080Ompcraft link signals|r\n" +
		"## Notes: Part of Ompcraft. Collapse this section; don't disable these.\n" +
		"## Group: OmpcraftSignals\n" +
		"## Category: Ompcraft link helpers\n" +
		"## LoadOnDemand: 1\n" +
		"## X-Ompcraft-Link: 0\n" +
		"## X-Ompcraft-Slots: 300\n" +
		"## X-Ompcraft-Signals: 2000\n"
	if got := SignalsTOC(300, 2000); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSlotTOC(t *testing.T) {
	want := "## Interface: 16001\n" +
		"## Title: |cff808080Ompcraft link slot|r\n" +
		"## Notes: Part of Ompcraft. Collapse this section; don't disable these.\n" +
		"## Group: OmpcraftS007\n" +
		"## Category: Ompcraft link helpers\n" +
		"## LoadOnDemand: 1\n" +
		"data.lua\n"
	if got := SlotTOC(7); got != want {
		t.Fatalf("got:\n%s", got)
	}
	// Slots differ only in the Group line: each is its own group parent, so the
	// client's list can collapse them under the shared category.
	a, b := strings.Split(SlotTOC(1), "\n"), strings.Split(SlotTOC(2), "\n")
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
			if !strings.HasPrefix(a[i], "## Group: OmpcraftS001") || !strings.HasPrefix(b[i], "## Group: OmpcraftS002") {
				t.Errorf("unexpected difference: %q vs %q", a[i], b[i])
			}
		}
	}
	if diff != 1 {
		t.Errorf("slot TOCs differ in %d lines, want 1", diff)
	}
}

func TestReadInstalled(t *testing.T) {
	a := t.TempDir()
	if _, err := ReadInstalled(a); err == nil {
		t.Fatal("expected a not-installed error")
	}
	dir := filepath.Join(a, SignalsName)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, SignalsName+".toc"), []byte(SignalsTOC(12, 34)), 0o644)
	l, err := ReadInstalled(a)
	if err != nil || l.Slots != 12 || l.Signals != 34 || l.Addons != a {
		t.Fatalf("got %+v, %v", l, err)
	}
	_ = os.WriteFile(filepath.Join(dir, SignalsName+".toc"), []byte("## Interface: 16001\n"), 0o644)
	if _, err := ReadInstalled(a); err == nil {
		t.Fatal("expected an error for a toc without counts")
	}
}

func TestDefaults(t *testing.T) {
	if DefaultSlots != 1000 || DefaultSignals != 2000 || MaxSlots != 1000 {
		t.Fatalf("defaults: slots=%d signals=%d max slots=%d", DefaultSlots, DefaultSignals, MaxSlots)
	}
}
