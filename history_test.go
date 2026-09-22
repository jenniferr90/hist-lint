package histlint

import (
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, input string, opts Options) *Result {
	t.Helper()
	res, err := Parse(strings.NewReader(input), opts)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	return res
}

func wantParseError(t *testing.T, input string, opts Options, wantLine int, wantSubstr string) {
	t.Helper()
	_, err := Parse(strings.NewReader(input), opts)
	if err == nil {
		t.Fatalf("Parse: expected error, got none")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("Parse: expected *ParseError, got %T: %v", err, err)
	}
	if pe.Line != wantLine {
		t.Errorf("ParseError.Line = %d, want %d", pe.Line, wantLine)
	}
	if !strings.Contains(pe.Message, wantSubstr) {
		t.Errorf("ParseError.Message = %q, want substring %q", pe.Message, wantSubstr)
	}
}

func TestParsePlain(t *testing.T) {
	input := "echo one\nls -la\ngit status\n"
	res := mustParse(t, input, Options{})

	if res.Format != FormatPlain {
		t.Fatalf("Format = %v, want %v", res.Format, FormatPlain)
	}
	want := []string{"echo one", "ls -la", "git status"}
	if len(res.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(res.Entries), len(want))
	}
	for i, w := range want {
		if res.Entries[i].Command != w {
			t.Errorf("entry %d: Command = %q, want %q", i, res.Entries[i].Command, w)
		}
		if res.Entries[i].Line != i+1 {
			t.Errorf("entry %d: Line = %d, want %d", i, res.Entries[i].Line, i+1)
		}
	}
	if len(res.Issues) != 0 {
		t.Errorf("Issues = %v, want none", res.Issues)
	}
}

func TestParsePlainBlankLineStrict(t *testing.T) {
	input := "echo one\n\nls -la\n"
	wantParseError(t, input, Options{}, 2, "blank line in plain history")
}

func TestParsePlainBlankLineLenient(t *testing.T) {
	input := "echo one\n\nls -la\n"
	res := mustParse(t, input, Options{Lenient: true})

	if len(res.Issues) != 1 || res.Issues[0].Line != 2 {
		t.Fatalf("Issues = %v, want one issue on line 2", res.Issues)
	}
	want := []string{"echo one", "ls -la"}
	if len(res.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(res.Entries), len(want))
	}
	for i, w := range want {
		if res.Entries[i].Command != w {
			t.Errorf("entry %d: Command = %q, want %q", i, res.Entries[i].Command, w)
		}
	}
}

func TestParseBashTimestamped(t *testing.T) {
	input := "#1000\necho one\n#2000\nls -la\n"
	res := mustParse(t, input, Options{})

	if res.Format != FormatBashTimestamped {
		t.Fatalf("Format = %v, want %v", res.Format, FormatBashTimestamped)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(res.Entries))
	}

	e0 := res.Entries[0]
	if e0.Command != "echo one" || e0.Line != 1 {
		t.Errorf("entry 0 = %+v, want Command=echo one Line=1", e0)
	}
	if !e0.Timestamp.Equal(time.Unix(1000, 0)) {
		t.Errorf("entry 0 Timestamp = %v, want %v", e0.Timestamp, time.Unix(1000, 0))
	}

	e1 := res.Entries[1]
	if e1.Command != "ls -la" || e1.Line != 3 {
		t.Errorf("entry 1 = %+v, want Command=ls -la Line=3", e1)
	}
	if !e1.Timestamp.Equal(time.Unix(2000, 0)) {
		t.Errorf("entry 1 Timestamp = %v, want %v", e1.Timestamp, time.Unix(2000, 0))
	}
}

func TestParseBashTimestampedMalformedStrict(t *testing.T) {
	input := "#1000\necho one\nnot-a-timestamp\necho two\n"
	wantParseError(t, input, Options{Format: FormatBashTimestamped}, 3, "expected a #<epoch> timestamp line")
}

func TestParseBashTimestampedMalformedLenient(t *testing.T) {
	// Once a #<epoch> line is missing, every subsequent line looks like
	// a command with no timestamp: each is recorded as its own issue.
	input := "#1000\necho one\nnot-a-timestamp\necho two\n"
	res := mustParse(t, input, Options{Format: FormatBashTimestamped, Lenient: true})

	if len(res.Issues) != 2 {
		t.Fatalf("Issues = %v, want 2", res.Issues)
	}
	if res.Issues[0].Line != 3 || res.Issues[1].Line != 4 {
		t.Errorf("Issues = %v, want lines 3 and 4", res.Issues)
	}

	want := []string{"echo one", "not-a-timestamp", "echo two"}
	if len(res.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(res.Entries), len(want))
	}
	for i, w := range want {
		if res.Entries[i].Command != w {
			t.Errorf("entry %d Command = %q, want %q", i, res.Entries[i].Command, w)
		}
	}
}

func TestParseBashTimestampedTrailingTimestamp(t *testing.T) {
	input := "#1000\necho one\n#2000\n"
	wantParseError(t, input, Options{Format: FormatBashTimestamped}, 3, "timestamp has no following command")
}

func TestParseZshExtended(t *testing.T) {
	input := ": 1000:0;echo one\n: 2000:5;ls -la\n"
	res := mustParse(t, input, Options{})

	if res.Format != FormatZshExtended {
		t.Fatalf("Format = %v, want %v", res.Format, FormatZshExtended)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(res.Entries))
	}

	e0 := res.Entries[0]
	if e0.Command != "echo one" || e0.Elapsed != 0 {
		t.Errorf("entry 0 = %+v", e0)
	}
	if !e0.Timestamp.Equal(time.Unix(1000, 0)) {
		t.Errorf("entry 0 Timestamp = %v, want %v", e0.Timestamp, time.Unix(1000, 0))
	}

	e1 := res.Entries[1]
	if e1.Command != "ls -la" || e1.Elapsed != 5*time.Second {
		t.Errorf("entry 1 = %+v", e1)
	}
}

func TestParseZshExtendedMultiline(t *testing.T) {
	input := ": 1000:0;echo one \\\ncontinued\n: 2000:0;ls -la\n"
	res := mustParse(t, input, Options{})

	if len(res.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(res.Entries))
	}
	want := "echo one \ncontinued"
	if res.Entries[0].Command != want {
		t.Errorf("entry 0 Command = %q, want %q", res.Entries[0].Command, want)
	}
	if res.Entries[0].Line != 1 {
		t.Errorf("entry 0 Line = %d, want 1", res.Entries[0].Line)
	}
	if res.Entries[1].Command != "ls -la" {
		t.Errorf("entry 1 Command = %q, want ls -la", res.Entries[1].Command)
	}
}

func TestParseZshExtendedOrphanContinuation(t *testing.T) {
	input := "not a header at all\n: 1000:0;echo one\n"
	wantParseError(t, input, Options{Format: FormatZshExtended}, 1, "line does not start a new entry and none is open")
}

func TestParseZshExtendedAbandonedContinuation(t *testing.T) {
	input := ": 1000:0;echo one\\\n: 2000:0;echo two\n"
	wantParseError(t, input, Options{}, 2, "previous entry ends with a trailing backslash but is never continued")
}

func TestParseZshExtendedAbandonedContinuationLenient(t *testing.T) {
	input := ": 1000:0;echo one\\\n: 2000:0;echo two\n"
	res := mustParse(t, input, Options{Lenient: true})

	if len(res.Issues) != 1 || res.Issues[0].Line != 2 {
		t.Fatalf("Issues = %v, want one issue on line 2", res.Issues)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(res.Entries))
	}
	if res.Entries[0].Command != "echo one" {
		t.Errorf("entry 0 Command = %q, want echo one", res.Entries[0].Command)
	}
	if res.Entries[1].Command != "echo two" {
		t.Errorf("entry 1 Command = %q, want echo two", res.Entries[1].Command)
	}
}

func TestParseZshExtendedUnterminatedAtEOF(t *testing.T) {
	input := ": 1000:0;echo one\\\n"
	wantParseError(t, input, Options{}, 1, "unterminated multi-line entry at end of file")
}

func TestParseZshExtendedUnterminatedAtEOFLenient(t *testing.T) {
	input := ": 1000:0;echo one\\\n"
	res := mustParse(t, input, Options{Lenient: true})

	if len(res.Issues) != 1 {
		t.Fatalf("Issues = %v, want 1", res.Issues)
	}
	if len(res.Entries) != 1 || res.Entries[0].Command != "echo one" {
		t.Fatalf("Entries = %+v, want single entry with Command=echo one", res.Entries)
	}
}

func TestDetectFormatFromFirstNonBlankLine(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Format
	}{
		{"plain", "\n\necho one\n", FormatPlain},
		{"bash-timestamped", "\n#1000\necho one\n", FormatBashTimestamped},
		{"zsh-extended", "\n: 1000:0;echo one\n", FormatZshExtended},
		{"all blank defaults to plain", "\n\n\n", FormatPlain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := Parse(strings.NewReader(c.input), Options{Lenient: true})
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			if res.Format != c.want {
				t.Errorf("Format = %v, want %v", res.Format, c.want)
			}
		})
	}
}

func TestFormatOverrideIgnoresDetection(t *testing.T) {
	// Looks like plain text but is forced to parse as zsh-extended, so
	// every line should be reported as an orphaned continuation.
	res, err := Parse(strings.NewReader("echo one\n"), Options{Format: FormatZshExtended, Lenient: true})
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if res.Format != FormatZshExtended {
		t.Fatalf("Format = %v, want %v", res.Format, FormatZshExtended)
	}
	if len(res.Issues) != 1 {
		t.Fatalf("Issues = %v, want 1", res.Issues)
	}
}

func TestFormatString(t *testing.T) {
	cases := map[Format]string{
		FormatPlain:           "plain",
		FormatBashTimestamped: "bash-timestamped",
		FormatZshExtended:     "zsh-extended",
		FormatUnknown:         "unknown",
		Format(99):            "unknown",
	}
	for f, want := range cases {
		if got := f.String(); got != want {
			t.Errorf("Format(%d).String() = %q, want %q", f, got, want)
		}
	}
}

func TestIssueString(t *testing.T) {
	iss := Issue{Line: 42, Message: "something is wrong"}
	want := "line 42: something is wrong"
	if got := iss.String(); got != want {
		t.Errorf("Issue.String() = %q, want %q", got, want)
	}
}

func TestDedupKeepsMostRecentOccurrence(t *testing.T) {
	entries := []Entry{
		{Command: "ls -la", Line: 1},
		{Command: "git status", Line: 2},
		{Command: "ls -la", Line: 3},
		{Command: "echo hi", Line: 4},
		{Command: "git status", Line: 5},
	}
	got := Dedup(entries)

	want := []Entry{
		{Command: "ls -la", Line: 3},
		{Command: "echo hi", Line: 4},
		{Command: "git status", Line: 5},
	}
	if len(got) != len(want) {
		t.Fatalf("Dedup returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Command != w.Command || got[i].Line != w.Line {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestDedupNoDuplicates(t *testing.T) {
	entries := []Entry{
		{Command: "a", Line: 1},
		{Command: "b", Line: 2},
	}
	got := Dedup(entries)
	if len(got) != 2 {
		t.Fatalf("Dedup = %+v, want unchanged", got)
	}
}

func TestDedupEmpty(t *testing.T) {
	got := Dedup(nil)
	if len(got) != 0 {
		t.Fatalf("Dedup(nil) = %+v, want empty", got)
	}
}

func TestFilterByTimeUnbounded(t *testing.T) {
	entries := []Entry{
		{Command: "a", Timestamp: time.Unix(1000, 0)},
		{Command: "b"}, // plain entry, no Timestamp
	}
	got := FilterByTime(entries, time.Time{}, time.Time{})
	if len(got) != len(entries) {
		t.Fatalf("FilterByTime with no bounds = %+v, want entries unchanged", got)
	}
}

func TestFilterByTimeSinceAndUntil(t *testing.T) {
	entries := []Entry{
		{Command: "before", Timestamp: time.Unix(500, 0)},
		{Command: "in-range-1", Timestamp: time.Unix(1000, 0)},
		{Command: "in-range-2", Timestamp: time.Unix(1500, 0)},
		{Command: "at-until", Timestamp: time.Unix(2000, 0)},
		{Command: "after", Timestamp: time.Unix(2500, 0)},
	}
	got := FilterByTime(entries, time.Unix(1000, 0), time.Unix(2000, 0))

	want := []string{"in-range-1", "in-range-2"}
	if len(got) != len(want) {
		t.Fatalf("FilterByTime = %+v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].Command != w {
			t.Errorf("entry %d Command = %q, want %q", i, got[i].Command, w)
		}
	}
}

func TestFilterByTimeSinceOnly(t *testing.T) {
	entries := []Entry{
		{Command: "before", Timestamp: time.Unix(500, 0)},
		{Command: "after", Timestamp: time.Unix(1500, 0)},
	}
	got := FilterByTime(entries, time.Unix(1000, 0), time.Time{})
	if len(got) != 1 || got[0].Command != "after" {
		t.Fatalf("FilterByTime = %+v, want only %q", got, "after")
	}
}

func TestFilterByTimeDropsEntriesWithoutTimestamp(t *testing.T) {
	entries := []Entry{
		{Command: "no-timestamp"},
		{Command: "has-timestamp", Timestamp: time.Unix(1000, 0)},
	}
	got := FilterByTime(entries, time.Unix(500, 0), time.Time{})
	if len(got) != 1 || got[0].Command != "has-timestamp" {
		t.Fatalf("FilterByTime = %+v, want only %q", got, "has-timestamp")
	}
}

func TestStatsCountsAndOrders(t *testing.T) {
	entries := []Entry{
		{Command: "git status"},
		{Command: "ls -la"},
		{Command: "git commit -m x"},
		{Command: "ls"},
		{Command: "git push"},
	}
	got := Stats(entries)

	want := []CommandStat{
		{Command: "git", Count: 3},
		{Command: "ls", Count: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("Stats = %+v, want %+v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestStatsTiesBrokenAlphabetically(t *testing.T) {
	entries := []Entry{
		{Command: "zsh -c foo"},
		{Command: "awk '{print}'"},
		{Command: "make build"},
	}
	got := Stats(entries)

	want := []string{"awk", "make", "zsh"}
	if len(got) != len(want) {
		t.Fatalf("Stats = %+v, want %d entries", got, len(want))
	}
	for i, w := range want {
		if got[i].Command != w || got[i].Count != 1 {
			t.Errorf("entry %d = %+v, want Command=%q Count=1", i, got[i], w)
		}
	}
}

func TestStatsMultilineCommandUsesFirstToken(t *testing.T) {
	entries := []Entry{
		{Command: "python3 <<'EOF'\nprint(1)\nEOF"},
		{Command: "python3 -c pass"},
	}
	got := Stats(entries)

	if len(got) != 1 || got[0].Command != "python3" || got[0].Count != 2 {
		t.Fatalf("Stats = %+v, want single entry Command=python3 Count=2", got)
	}
}

func TestStatsEmpty(t *testing.T) {
	got := Stats(nil)
	if len(got) != 0 {
		t.Fatalf("Stats(nil) = %+v, want empty", got)
	}
}

func TestParseEmptyInput(t *testing.T) {
	res := mustParse(t, "", Options{})
	if res.Format != FormatPlain {
		t.Errorf("Format = %v, want %v", res.Format, FormatPlain)
	}
	if len(res.Entries) != 0 || len(res.Issues) != 0 {
		t.Errorf("Entries/Issues = %v/%v, want both empty", res.Entries, res.Issues)
	}
}
