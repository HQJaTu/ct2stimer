package crontab

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/robfig/cron"
)

const (
	minMinute = 0
	maxMinute = 59
	minHour   = 0
	maxHour   = 23
	minDom    = 1
	maxDom    = 31
	minMonth  = 1
	maxMonth  = 12
	minDow    = 0
	maxDow    = 6

	minSecond = 0
	maxSecond = 59
)

var (
	suffixRegexp = regexp.MustCompile(`[^a-zA-Z0-9]+$`)
	// envRegexp matches crontab environment-variable assignment lines such as
	// "SHELL=/bin/bash", "PATH = /sbin:/bin" or `MAILTO=""` (man 5 crontab).
	// A schedule line can never match this, because its first field is always a
	// digit, "*" or "@" -- never a bare identifier followed by "=".
	envRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=`)
	// timerNameRegexp validates a TimerName from a config comment. The value
	// becomes a systemd unit file name, so it is restricted to a safe subset.
	timerNameRegexp = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	weekDays        = []string{
		"Sun",
		"Mon",
		"Tue",
		"Wed",
		"Thu",
		"Fri",
		"Sat",
	}
)

// Config holds the optional per-entry configuration supplied through a
// "# config:" comment placed above a crontab entry (see Parse). Its zero value
// means "no configuration": TimerName empty and RunSecond nil.
type Config struct {
	// TimerName overrides the generated systemd unit name for this entry.
	TimerName string
	// RunSecond, when set, shifts the trigger to this second-of-the-minute
	// (0-59). A pointer is used so that "unset" is distinguishable from the
	// legitimate value 0.
	RunSecond *int
}

// Schedule represents crontab spec and command
type Schedule struct {
	Spec    string
	Command string
	Config  Config
}

// Parse parses crontab file and return a list of Schedule, plus any non-fatal
// warnings (e.g. unrecognised config keys) for the caller to surface.
//
// Blank lines, comments (#...) and environment-variable assignments
// (e.g. SHELL=/bin/bash) are skipped, matching the line types that a real
// crontab -- including /etc/crontab -- legitimately contains (man 5 crontab).
// Both standard 5-field specs and "@" descriptors (@daily, @every 1h30m, ...)
// are recognised, and fields may be separated by spaces or tabs.
//
// A "# config: Key=Value ..." comment attaches per-entry configuration (see
// Config) to the next schedule entry; intervening blank lines and ordinary
// comments are tolerated. A config comment with no following entry is an error.
//
// A line that looks like a schedule but cannot be tokenised is reported with
// its line number and content rather than silently dropped, since a missed
// line would mean a scheduled job never gets created.
func Parse(crontab string) ([]*Schedule, []string, error) {
	schedules := []*Schedule{}
	warnings := []string{}
	lines := strings.Split(crontab, "\n")

	var pending *Config
	var pendingLine int

	for i, raw := range lines {
		line := strings.TrimSpace(raw)

		// Comments: a "config:" comment carries per-entry configuration; any
		// other comment is inert.
		if strings.HasPrefix(line, "#") {
			body, ok := configCommentBody(line)
			if !ok {
				continue
			}

			if pending != nil {
				return []*Schedule{}, nil, errors.Errorf("crontab line %d: config comment is not attached to a schedule entry", pendingLine)
			}

			cfg, warns, err := parseConfigComment(body)
			if err != nil {
				return []*Schedule{}, nil, errors.Wrapf(err, "crontab line %d", i+1)
			}
			for _, w := range warns {
				warnings = append(warnings, fmt.Sprintf("crontab line %d: %s", i+1, w))
			}

			pending = &cfg
			pendingLine = i + 1
			continue
		}

		// Blank lines and environment-variable assignments carry no schedule,
		// but they do not detach a pending config from the entry below them.
		if line == "" || envRegexp.MatchString(line) {
			continue
		}

		spec, command, err := splitSpecCommand(line)
		if err != nil {
			return []*Schedule{}, nil, errors.Wrapf(err, "crontab line %d: %q", i+1, line)
		}

		schedule := &Schedule{
			Spec:    spec,
			Command: command,
		}
		if pending != nil {
			schedule.Config = *pending
			pending = nil
		}

		schedules = append(schedules, schedule)
	}

	if pending != nil {
		return []*Schedule{}, nil, errors.Errorf("crontab line %d: config comment is not attached to a schedule entry", pendingLine)
	}

	return schedules, warnings, nil
}

// configCommentBody reports whether line is a "# config:" comment and, if so,
// returns the trimmed text following the "config:" marker. The "#" and the
// "config:" marker are both matched case-insensitively and tolerate missing or
// repeated whitespace (e.g. "#config:", "#  Config:  ...").
func configCommentBody(line string) (string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "#"))

	const marker = "config:"
	if len(rest) < len(marker) || !strings.EqualFold(rest[:len(marker)], marker) {
		return "", false
	}

	return strings.TrimSpace(rest[len(marker):]), true
}

// parseConfigComment parses the "Key=Value Key=Value" body of a config comment.
// Recognised keys are TimerName and RunSecond; unknown keys yield a warning
// (and are otherwise ignored) so that older binaries tolerate newer configs.
func parseConfigComment(body string) (Config, []string, error) {
	cfg := Config{}
	warnings := []string{}

	for _, field := range strings.Fields(body) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" {
			return Config{}, nil, errors.Errorf("malformed config item %q (expected Key=Value)", field)
		}

		switch key {
		case "TimerName":
			if !timerNameRegexp.MatchString(value) {
				return Config{}, nil, errors.Errorf("invalid TimerName %q (allowed characters: letters, digits, '.', '_', '-')", value)
			}
			cfg.TimerName = value
		case "RunSecond":
			second, err := strconv.Atoi(value)
			if err != nil || second < minSecond || second > maxSecond {
				return Config{}, nil, errors.Errorf("invalid RunSecond %q (expected an integer 0-59)", value)
			}
			cfg.RunSecond = &second
		default:
			warnings = append(warnings, fmt.Sprintf("ignoring unknown config key %q", key))
		}
	}

	return cfg, warnings, nil
}

// splitSpecCommand splits a single crontab line into its schedule spec and the
// command to run. It understands standard 5-field specs as well as "@"
// descriptors, and tolerates space- or tab-separated fields.
func splitSpecCommand(line string) (spec, command string, err error) {
	if strings.HasPrefix(line, "@") {
		// Descriptors are a single field, except "@every" which takes a
		// duration argument (e.g. "@every 1h30m") as part of the spec.
		n := 1
		head := line
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			head = line[:i]
		}
		if strings.EqualFold(head, "@every") {
			n = 2
		}

		fields, rest, ok := takeFields(line, n)
		if !ok || rest == "" {
			return "", "", errors.New("schedule descriptor has no command")
		}

		return strings.Join(fields, " "), rest, nil
	}

	fields, rest, ok := takeFields(line, 5)
	if !ok {
		return "", "", errors.Errorf("expected 5 schedule fields followed by a command, got %d field(s)", len(fields))
	}
	if rest == "" {
		return "", "", errors.New("schedule has no command")
	}

	return strings.Join(fields, " "), rest, nil
}

// takeFields consumes the first n whitespace-separated fields of s (treating
// runs of spaces and tabs as a single separator) and returns those fields plus
// the unconsumed remainder, with the remainder's internal spacing preserved.
// ok is false when s contains fewer than n fields.
func takeFields(s string, n int) (fields []string, rest string, ok bool) {
	i := 0

	for len(fields) < n {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return fields, "", false
		}

		start := i
		for i < len(s) && !isSpace(s[i]) {
			i++
		}
		fields = append(fields, s[start:i])
	}

	for i < len(s) && isSpace(s[i]) {
		i++
	}

	return fields, s[i:], true
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t'
}

// ConvertToSystemdCalendar converts crontab spec format to Systemd Timer format
//
//	crontab:       https://en.wikipedia.org/wiki/Cron
//	Systemd Timer: https://www.freedesktop.org/software/systemd/man/systemd.time.html
func (s *Schedule) ConvertToSystemdCalendar() (string, error) {
	schedule, err := cron.ParseStandard(s.Spec)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse schedule spec %q", s.Spec)
	}

	specSchedule, ok := schedule.(*cron.SpecSchedule)
	if !ok {
		return "", errors.New("unable to convert Schedule to SpecSchedule")
	}

	minutes := parseBits(specSchedule.Minute, minMinute, maxMinute)
	hours := parseBits(specSchedule.Hour, minHour, maxHour)
	doms := parseBits(specSchedule.Dom, minDom, maxDom)
	months := parseBits(specSchedule.Month, minMonth, maxMonth)
	dows := parseBits(specSchedule.Dow, minDow, maxDow)

	fields := []string{}

	if dows != "*" {
		weekdays, err := convertDowsToWeekdays(dows)
		if err != nil {
			return "", errors.Wrap(err, "failed to convert day of weeks")
		}
		fields = append(fields, weekdays)
	}

	if months != "*" || doms != "*" {
		fields = append(fields, fmt.Sprintf("%s-%s", months, doms))
	}

	// crontab has minute resolution, so the second defaults to :00. A config
	// comment may pin a specific second to dodge collisions with other jobs.
	if s.Config.RunSecond != nil {
		fields = append(fields, fmt.Sprintf("%s:%s:%02d", hours, minutes, *s.Config.RunSecond))
	} else {
		fields = append(fields, fmt.Sprintf("%s:%s", hours, minutes))
	}

	return strings.Join(fields, " "), nil
}

// NameByRegexp returns schedule name extracted by the given regexp
func (s *Schedule) NameByRegexp(nameRegexp *regexp.Regexp) string {
	if nameRegexp == nil {
		return ""
	}

	var name string

	match := nameRegexp.FindStringSubmatch(s.Command)
	if len(match) >= 2 {
		name = match[1]
	} else {
		name = ""
	}

	return suffixRegexp.ReplaceAllString(name, "")
}

// SHA256Sum generates SHA-256 checksum of schedule
func (s *Schedule) SHA256Sum() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s;%s", s.Spec, s.Command))))
}

func convertDowsToWeekdays(bits string) (string, error) {
	dows := []string{}

	for _, bit := range strings.Split(bits, ",") {
		b, err := strconv.Atoi(bit)
		if err != nil {
			return "", errors.Wrap(err, "failed to parse bit string")
		}
		dows = append(dows, weekDays[b])
	}

	return strings.Join(dows, ","), nil
}

func parseBits(n uint64, min, max int) string {
	var all1 uint64

	for i := min; i <= max; i++ {
		all1 |= 1 << uint(i)
	}

	if n&all1 == all1 {
		return "*"
	}

	bits := []string{}

	for i := 0; i <= max; i++ {
		if n&(1<<uint(i)) > 0 {
			bits = append(bits, strconv.Itoa(i))
		}
	}

	return strings.Join(bits, ",")
}
