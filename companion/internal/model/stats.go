package model

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	durationRe   = regexp.MustCompile(`^(?:(\d+)\s*h)?\s*(?:(\d+)\s*m(?:in)?)?\s*(?:(\d+(?:\.\d+)?)\s*s(?:ec)?)?$`)
	millisRe     = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*ms$`)
	thisTurnRe   = regexp.MustCompile(`(?i)\s*\bthis turn\s*$`)
	tokensRe     = regexp.MustCompile(`(?i)\btok(?:ens?)?\b`)
	maxStatsPart = 24
)

// FormatDuration renders a duration part of omp's footer ("43.2s", "1m 12s",
// "1h 4m 3s", "850ms") as "43s", "1m 12s" or "1h 4m". ok is false if s is not
// a duration.
func FormatDuration(s string) (string, bool) {
	s = strings.TrimSpace(s)
	var total float64
	if m := millisRe.FindStringSubmatch(s); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		total = v / 1000
	} else {
		m := durationRe.FindStringSubmatch(s)
		if m == nil || (m[1] == "" && m[2] == "" && m[3] == "") {
			return "", false
		}
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		sec, _ := strconv.ParseFloat(m[3], 64)
		total = float64(h*3600+mi*60) + sec
	}
	n := int64(math.Floor(total + 0.5))
	h, mi, sec := n/3600, n%3600/60, n%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, mi), true
	case mi > 0:
		return fmt.Sprintf("%dm %ds", mi, sec), true
	}
	return fmt.Sprintf("%ds", sec), true
}

// Stats parses omp's turn footer out of a last_message ("answer\n·\n43.2s ·
// 22K tok · $1.09 this turn") and returns "1m 12s · 22K tok · $1.09"-style
// text: durations reformatted, "this turn" dropped, parts joined with " · ".
// It returns "" when there is no footer or none of its parts is recognisable
// as a duration, token count or cost. Unrecognised short parts next to
// recognised ones are kept; long ones are dropped.
func Stats(lastMessage string) string {
	_, footer := splitFooter(Clean(lastMessage))
	footer = strings.TrimSpace(strings.ReplaceAll(footer, "\n", " "))
	if footer == "" {
		return ""
	}
	var parts []string
	recognised := false
	for _, p := range strings.Split(footer, "·") {
		p = strings.TrimSpace(thisTurnRe.ReplaceAllString(strings.TrimSpace(p), ""))
		if p == "" {
			continue
		}
		if d, ok := FormatDuration(p); ok {
			parts = append(parts, d)
			recognised = true
			continue
		}
		if strings.HasPrefix(p, "$") || tokensRe.MatchString(p) {
			recognised = true
		} else if len(p) > maxStatsPart {
			continue
		}
		parts = append(parts, p)
	}
	if !recognised {
		return ""
	}
	return strings.Join(parts, " · ")
}
