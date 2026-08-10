package cli

import (
	"regexp"
	"strconv"
	"strings"
)

var isoDurationRe = regexp.MustCompile(`^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// formatISODuration renders an ISO 8601 duration as human text, returning the input unchanged if it can't parse.
func formatISODuration(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}
	m := isoDurationRe.FindStringSubmatch(trimmed)
	if m == nil {
		return s
	}

	years := atoiOrZero(m[1])
	months := atoiOrZero(m[2])
	weeks := atoiOrZero(m[3])
	days := atoiOrZero(m[4])
	hours := atoiOrZero(m[5])
	minutes := atoiOrZero(m[6])
	seconds := atoiOrZero(m[7])

	total := years + months + weeks + days + hours + minutes + seconds
	if total == 0 {
		if !strings.ContainsAny(trimmed, "0123456789") {
			return s
		}
		return "none"
	}

	if weeks == 0 && days > 0 && days%7 == 0 && years+months+hours+minutes+seconds == 0 {
		weeks = days / 7
		days = 0
	}

	var parts []string
	add := func(n int, unit string) {
		if n > 0 {
			parts = append(parts, pluralize(n, unit))
		}
	}
	add(years, "year")
	add(months, "month")
	add(weeks, "week")
	add(days, "day")
	add(hours, "hour")
	add(minutes, "minute")
	add(seconds, "second")

	return strings.Join(parts, " ")
}

func atoiOrZero(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func pluralize(n int, unit string) string {
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s
}
