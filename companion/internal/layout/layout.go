// Package layout defines the names and file contents the companion installs
// under A (G/Interface/AddOns), per PROTOCOL §1.
package layout

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	AddonName   = "Ompcraft"
	SignalsName = "OmpcraftSignals"
	AddonTmp    = ".Ompcraft.tmp"
	SlotPrefix  = "OmpcraftS"
	CtlPresent  = "ctl_present.wav"
	DataFile    = "data.lua"
	DataTmp     = "data.lua.tmp"
	EmptyData   = "OmpcraftInbox = nil\n"

	DefaultSlots   = 1000
	DefaultSignals = 2000
	MaxSlots       = 1000  // slot names carry 3 digits
	MaxSignals     = 10000 // doorbell names carry 4 digits
)

var (
	slotNameRe = regexp.MustCompile(`^OmpcraftS[0-9]{3}$`)
	wavNameRe  = regexp.MustCompile(`^[0-9]{4}\.wav$`)
)

// SlotName returns "OmpcraftS007" for 7.
func SlotName(i int) string { return fmt.Sprintf("%s%03d", SlotPrefix, i) }

// DoorbellName returns "0007.wav" for 7.
func DoorbellName(i int) string { return fmt.Sprintf("%04d.wav", i) }

// IsSlotName reports whether name is OmpcraftS followed by exactly 3 digits.
func IsSlotName(name string) bool { return slotNameRe.MatchString(name) }

// IsDoorbellName reports whether name is a four-digit doorbell file name.
func IsDoorbellName(name string) bool { return wavNameRe.MatchString(name) }

// SlotIndex parses "OmpcraftS007" to 7.
func SlotIndex(name string) (int, bool) {
	if !IsSlotName(name) {
		return 0, false
	}
	n, err := strconv.Atoi(name[len(SlotPrefix):])
	return n, err == nil
}

// HelperCategory is the collapsible section the client's AddOns list shows
// the helpers under. Blizzard's list offers a collapsible node only for a
// `## Category:` whose addon is its own group parent (Group == addon name),
// so every helper is its own group.
const HelperCategory = "Ompcraft link helpers"

const helperNotes = "Part of Ompcraft. Collapse this section; don't disable these."

// SlotTOC returns the TOC of slot i. Slots differ only in the Group line.
func SlotTOC(i int) string {
	return "## Interface: 16001\n" +
		"## Title: |cff808080Ompcraft link slot|r\n" +
		"## Notes: " + helperNotes + "\n" +
		"## Group: " + SlotName(i) + "\n" +
		"## Category: " + HelperCategory + "\n" +
		"## LoadOnDemand: 1\n" +
		"data.lua\n"
}

// SignalsTOC returns OmpcraftSignals.toc for the given counts.
func SignalsTOC(slots, signals int) string {
	return "## Interface: 16001\n" +
		"## Title: |cff808080Ompcraft link signals|r\n" +
		"## Notes: " + helperNotes + "\n" +
		"## Group: " + SignalsName + "\n" +
		"## Category: " + HelperCategory + "\n" +
		"## LoadOnDemand: 1\n" +
		"## X-Ompcraft-Link: 0\n" +
		fmt.Sprintf("## X-Ompcraft-Slots: %d\n", slots) +
		fmt.Sprintf("## X-Ompcraft-Signals: %d\n", signals)
}

// Layout describes an installed link under one AddOns directory.
type Layout struct {
	Addons  string // A
	Slots   int    // N
	Signals int    // M
}

func (l Layout) AddonDir() string   { return filepath.Join(l.Addons, AddonName) }
func (l Layout) SignalsDir() string { return filepath.Join(l.Addons, SignalsName) }
func (l Layout) SlotDir(i int) string {
	return filepath.Join(l.Addons, SlotName(i))
}
func (l Layout) SlotData(i int) string { return filepath.Join(l.SlotDir(i), DataFile) }
func (l Layout) Doorbell(i int) string {
	return filepath.Join(l.SignalsDir(), DoorbellName(i))
}

// AddonsDir returns G/Interface/AddOns.
func AddonsDir(game string) string {
	return filepath.Join(game, "Interface", "AddOns")
}

// ReadInstalled reads N and M from the installed OmpcraftSignals.toc.
func ReadInstalled(addons string) (Layout, error) {
	p := filepath.Join(addons, SignalsName, SignalsName+".toc")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Layout{}, fmt.Errorf("ompcraft is not installed in %s (run `ompcraft install` first)", addons)
		}
		return Layout{}, err
	}
	l := Layout{Addons: addons, Slots: -1, Signals: -1}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "## X-Ompcraft-Slots:"); ok {
			l.Slots, _ = strconv.Atoi(strings.TrimSpace(v))
		}
		if v, ok := strings.CutPrefix(line, "## X-Ompcraft-Signals:"); ok {
			l.Signals, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if l.Slots < 1 || l.Slots > MaxSlots || l.Signals < 1 || l.Signals > MaxSignals {
		return Layout{}, fmt.Errorf("%s: missing or invalid X-Ompcraft-Slots/X-Ompcraft-Signals", p)
	}
	return l, nil
}

// SilentWAV returns a valid RIFF/WAVE PCM file: 8 kHz, mono, 8-bit, 50 ms of
// silence (unsigned 8-bit silence is 0x80).
func SilentWAV() []byte {
	const (
		rate    = 8000
		samples = rate / 20 // 50 ms
	)
	var b bytes.Buffer
	le := binary.LittleEndian
	w := func(v any) { _ = binary.Write(&b, le, v) }
	b.WriteString("RIFF")
	w(uint32(36 + samples))
	b.WriteString("WAVEfmt ")
	w(uint32(16))   // fmt chunk size
	w(uint16(1))    // PCM
	w(uint16(1))    // mono
	w(uint32(rate)) // sample rate
	w(uint32(rate)) // byte rate = rate * channels * bytes
	w(uint16(1))    // block align
	w(uint16(8))    // bits per sample
	b.WriteString("data")
	w(uint32(samples))
	b.Write(bytes.Repeat([]byte{0x80}, samples))
	return b.Bytes()
}

// TOCVersion returns the value of the "## Version:" line of a TOC file, or "".
func TOCVersion(toc []byte) string {
	for _, line := range strings.Split(string(toc), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "## Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
