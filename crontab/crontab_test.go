package crontab

import (
	"io/ioutil"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	filename := filepath.Join("..", "testdata", "crontab")
	body, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to open testdata. filename: %s, error: %s", filename, err)
	}

	expected := []*Schedule{
		&Schedule{
			Spec:    "0,5,10,15,20,25,30,35,40,45,50,55 * * * *",
			Command: "/bin/bash -l -c 'docker run --rm=true --name scheduler.task01.`date +\\%Y\\%m\\%d\\%H\\%M` --memory=5g 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/app:latest bundle exec rake task01 RAILS_ENV=production'",
		},
		&Schedule{
			Spec:    "15 * * * *",
			Command: "/bin/bash -l -c 'docker run --rm=true --name scheduler.task02.`date +\\%Y\\%m\\%d\\%H\\%M` --memory=5g 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/app:latest bundle exec rake task02 RAILS_ENV=production'",
		},
		&Schedule{
			Spec:    "30 * * * *",
			Command: "/bin/bash -l -c 'docker run --rm=true --name scheduler.task04.`date +\\%Y\\%m\\%d\\%H\\%M` --memory=5g 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/app:latest bundle exec rake task04 RAILS_ENV=production'",
		},
	}

	got, _, err := Parse(string(body))
	if err != nil {
		t.Errorf("Error should not be raised. error: %s", err)
	}

	if !reflect.DeepEqual(got, expected) {
		t.Errorf("Schedules do not match.\n  expected: %v\n  got:      %v", expected, got)
	}
}

func TestParseTolerant(t *testing.T) {
	// A crontab exercising every line type a real file may contain:
	// env assignments (with and without spaces around =), comments, blank
	// lines, tab-separated fields, multiple spaces and "@" descriptors.
	input := strings.Join([]string{
		"SHELL=/bin/bash",
		"PATH = /usr/sbin:/usr/bin",
		`MAILTO=""`,
		"# a comment",
		"",
		"   ",
		"5 * * * * /bin/echo space",
		"15\t*\t*\t*\t*\t/bin/echo tabs",
		"0   12   *   *   *   /bin/echo multi  space  command",
		"@daily /bin/echo daily",
		"@every 1h30m /bin/echo every",
	}, "\n")

	expected := []*Schedule{
		{Spec: "5 * * * *", Command: "/bin/echo space"},
		{Spec: "15 * * * *", Command: "/bin/echo tabs"},
		{Spec: "0 12 * * *", Command: "/bin/echo multi  space  command"},
		{Spec: "@daily", Command: "/bin/echo daily"},
		{Spec: "@every 1h30m", Command: "/bin/echo every"},
	}

	got, _, err := Parse(input)
	if err != nil {
		t.Fatalf("Error should not be raised. error: %s", err)
	}

	if !reflect.DeepEqual(got, expected) {
		t.Errorf("Schedules do not match.\n  expected: %v\n  got:      %v", expected, got)
	}
}

func TestParseInvalid(t *testing.T) {
	testcases := []struct {
		name  string
		input string
	}{
		{
			name:  "too few schedule fields",
			input: "0 5 broken",
		},
		{
			name:  "schedule without a command",
			input: "*/5 * * * *",
		},
		{
			name:  "descriptor without a command",
			input: "@daily",
		},
	}

	for _, tc := range testcases {
		got, _, err := Parse(tc.input)
		if err == nil {
			t.Errorf("%s: error should be raised, got schedules: %v", tc.name, got)
			continue
		}

		// The error must name the line number so the user can find it.
		if !strings.Contains(err.Error(), "line 1") {
			t.Errorf("%s: error should reference the line number, got: %s", tc.name, err)
		}
	}
}

func intPtr(n int) *int { return &n }

func TestParseConfig(t *testing.T) {
	// A config comment attaches to the next entry even across a blank line and
	// an ordinary comment; an entry with no config comment stays unconfigured.
	input := strings.Join([]string{
		"# config: TimerName=db-backup RunSecond=30",
		"# an ordinary comment in between",
		"",
		"*/5 * * * * /usr/local/bin/backup",
		"15 * * * * /bin/echo plain",
	}, "\n")

	expected := []*Schedule{
		{
			Spec:    "*/5 * * * *",
			Command: "/usr/local/bin/backup",
			Config:  Config{TimerName: "db-backup", RunSecond: intPtr(30)},
		},
		{
			Spec:    "15 * * * *",
			Command: "/bin/echo plain",
		},
	}

	got, warnings, err := Parse(input)
	if err != nil {
		t.Fatalf("Error should not be raised. error: %s", err)
	}
	if len(warnings) != 0 {
		t.Errorf("No warnings expected, got: %v", warnings)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("Schedules do not match.\n  expected: %v\n  got:      %v", expected, got)
	}
}

func TestParseConfigUnknownKeyWarns(t *testing.T) {
	input := "# config: TimreName=oops\n*/5 * * * * /bin/echo hi\n"

	got, warnings, err := Parse(input)
	if err != nil {
		t.Fatalf("Unknown key should warn, not error. error: %s", err)
	}
	if len(got) != 1 || got[0].Config.TimerName != "" {
		t.Errorf("Unknown key should be ignored, got config: %+v", got[0].Config)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "line 1") || !strings.Contains(warnings[0], "TimreName") {
		t.Errorf("Expected one warning referencing line 1 and the bad key, got: %v", warnings)
	}
}

func TestParseConfigInvalid(t *testing.T) {
	testcases := []struct {
		name  string
		input string
	}{
		{"RunSecond out of range", "# config: RunSecond=60\n*/5 * * * * /bin/echo hi\n"},
		{"RunSecond not a number", "# config: RunSecond=half\n*/5 * * * * /bin/echo hi\n"},
		{"TimerName with slash", "# config: TimerName=a/b\n*/5 * * * * /bin/echo hi\n"},
		{"malformed item", "# config: TimerName\n*/5 * * * * /bin/echo hi\n"},
		{"dangling config (no entry)", "# config: RunSecond=30\n"},
		{"two configs in a row", "# config: RunSecond=30\n# config: RunSecond=40\n*/5 * * * * /bin/echo hi\n"},
	}

	for _, tc := range testcases {
		_, _, err := Parse(tc.input)
		if err == nil {
			t.Errorf("%s: error should be raised", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "line ") {
			t.Errorf("%s: error should reference a line number, got: %s", tc.name, err)
		}
	}
}

func TestConvertToSystemdCalendarRunSecond(t *testing.T) {
	schedule := &Schedule{
		Spec:    "*/5 * * * *",
		Command: "/bin/echo hi",
		Config:  Config{RunSecond: intPtr(30)},
	}
	expected := "*:0,5,10,15,20,25,30,35,40,45,50,55:30"

	got, err := schedule.ConvertToSystemdCalendar()
	if err != nil {
		t.Fatalf("Error should not be raised. error: %s", err)
	}
	if got != expected {
		t.Errorf("Calendar does not match. expected: %q, actual: %q", expected, got)
	}
}

func TestConvertToSystemdCalendar(t *testing.T) {
	testcases := []struct {
		schedule *Schedule
		expected string
	}{
		{
			schedule: &Schedule{
				Spec:    "*/5 * * * *",
				Command: "",
			},
			expected: "*:0,5,10,15,20,25,30,35,40,45,50,55",
		},
		{
			schedule: &Schedule{
				Spec:    "0,5,10,15,20,25,30,35,40,45,50,55 10-12 * * *",
				Command: "",
			},
			expected: "10,11,12:0,5,10,15,20,25,30,35,40,45,50,55", // TODO: 10-12:0,5,...
		},
		{
			schedule: &Schedule{
				Spec:    "0-5 * 1 * *",
				Command: "",
			},
			expected: "*-1 *:0,1,2,3,4,5", // TODO: *:0-5
		},
		{
			schedule: &Schedule{
				Spec:    "23 2,1 * 12 1,6",
				Command: "",
			},
			expected: "Mon,Sat 12-* 1,2:23",
		},
		{
			schedule: &Schedule{
				Spec:    "0,20,40 8-17 * * 1-5",
				Command: "",
			},
			expected: "Mon,Tue,Wed,Thu,Fri 8,9,10,11,12,13,14,15,16,17:0,20,40",
		},
		{
			schedule: &Schedule{
				Spec:    "0 17 * * *",
				Command: "",
			},
			expected: "17:0",
		},
		{
			schedule: &Schedule{
				Spec:    "* * * * 0",
				Command: "",
			},
			expected: "Sun *:*",
		},
		{
			schedule: &Schedule{
				Spec:    "5 * * * *",
				Command: "",
			},
			expected: "*:5",
		},
	}

	for _, tc := range testcases {
		got, err := tc.schedule.ConvertToSystemdCalendar()
		if err != nil {
			t.Errorf("Error should not be raised. error: %s", err)
		}

		if got != tc.expected {
			t.Errorf("Calendar does not match. expected: %q, actual: %q", tc.expected, got)
		}
	}
}

func TestNameByRegexp(t *testing.T) {
	testcases := []struct {
		schedule   *Schedule
		nameRegexp *regexp.Regexp
		expected   string
	}{
		{
			schedule: &Schedule{
				Spec:    "0,5,10,15,20,25,30,35,40,45,50,55 * * * *",
				Command: "/bin/bash -l -c 'docker run --rm=true --name scheduler.task01.`date +\\%Y\\%m\\%d\\%H\\%M` --memory=5g 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/app:latest bundle exec rake task01 RAILS_ENV=production'",
			},
			nameRegexp: regexp.MustCompile(`--name ([a-zA-Z0-9.]+)`),
			expected:   "scheduler.task01",
		},
		{
			schedule: &Schedule{
				Spec:    "15 * * * *",
				Command: "/bin/echo hello",
			},
			nameRegexp: regexp.MustCompile(`--name ([a-zA-Z0-9.]+)`),
			expected:   "",
		},
		{
			schedule: &Schedule{
				Spec:    "30 * * * *",
				Command: "/bin/docker run --name hello ubuntu:16.04 echo hello",
			},
			nameRegexp: regexp.MustCompile(`--name ([a-zA-Z0-9.]+)`),
			expected:   "hello",
		},
		{
			schedule: &Schedule{
				Spec:    "30 * * * *",
				Command: "/bin/bash -l -c 'docker run --rm=true --name scheduler.task01_--.._.`date +\\%Y\\%m\\%d\\%H\\%M` --memory=5g 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/app:latest bundle exec rake task01 RAILS_ENV=production'",
			},
			nameRegexp: regexp.MustCompile(`--name ([a-zA-Z0-9.]+)`),
			expected:   "scheduler.task01",
		},
		{
			schedule: &Schedule{
				Spec:    "30 * * * *",
				Command: "/bin/bash -l -c 'docker run --rm=true --name scheduler.task01.`date +\\%Y\\%m\\%d\\%H\\%M` --memory=5g 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/app:latest bundle exec rake task01 RAILS_ENV=production'",
			},
			nameRegexp: regexp.MustCompile(``),
			expected:   "",
		},
		{
			schedule: &Schedule{
				Spec:    "30 * * * *",
				Command: "/bin/bash -l -c 'docker run --rm=true --name scheduler.task01.`date +\\%Y\\%m\\%d\\%H\\%M` --memory=5g 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/app:latest bundle exec rake task01 RAILS_ENV=production'",
			},
			nameRegexp: nil,
			expected:   "",
		},
	}

	for _, tc := range testcases {
		if got := tc.schedule.NameByRegexp(tc.nameRegexp); got != tc.expected {
			t.Errorf("Name does not match. expected: %q, got: %q", tc.expected, got)
		}
	}
}

func TestSHA256Sum(t *testing.T) {
	schedule := &Schedule{
		Spec:    "15 * * * *",
		Command: "echo 'hello'",
	}
	expected := "4ab7fd35a3996a8b58483a640a52976d5c974372c12e5f7a973be86d96a0096e"

	if got := schedule.SHA256Sum(); got != expected {
		t.Errorf("Checksum does not match. expected: %q, got: %q", expected, got)
	}
}
