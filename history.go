// Package histlint parses shell history files (plain bash, timestamped
// bash, and zsh extended history) into structured entries, and reports
// on lines that don't fit the format they're supposed to be in.
//
// History files are line-oriented but not always one-command-per-line:
// zsh escapes embedded newlines with a trailing backslash, and bash just
// writes them raw. A naive line splitter silently mangles multi-line
// commands in both cases. histlint parses strictly by default and returns
// an error on the first line it can't account for, rather than guessing.
package histlint

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Format identifies which on-disk shell history layout a file uses.
type Format int

const (
	FormatUnknown Format = iota
	FormatPlain
	FormatBashTimestamped
	FormatZshExtended
)

func (f Format) String() string {
	switch f {
	case FormatPlain:
		return "plain"
	case FormatBashTimestamped:
		return "bash-timestamped"
	case FormatZshExtended:
		return "zsh-extended"
	default:
		return "unknown"
	}
}

// Entry is a single command pulled from a history file. Command preserves
// embedded newlines for multi-line commands exactly as they were run.
type Entry struct {
	Command   string
	Timestamp time.Time     `json:",omitempty"`
	Elapsed   time.Duration `json:",omitempty"` // zsh only
	Line      int           // 1-based line the entry starts on
}

// Issue describes a line that didn't fit the detected format.
type Issue struct {
	Line    int
	Message string
}

func (i Issue) String() string {
	return fmt.Sprintf("line %d: %s", i.Line, i.Message)
}

// ParseError is returned by Parse in strict mode on the first Issue found.
type ParseError struct {
	Issue
}

func (e *ParseError) Error() string {
	return "histlint: " + e.Issue.String()
}

// Options controls how Parse treats malformed input.
type Options struct {
	// Lenient, when true, tolerates malformed lines instead of stopping
	// on the first one: bad lines are recorded in Result.Issues and
	// parsing continues on a best-effort basis. The zero value is strict.
	Lenient bool

	// Format overrides auto-detection. Leave as FormatUnknown to detect
	// the format from the file's first non-blank line.
	Format Format
}

// Result is the outcome of a successful Parse.
type Result struct {
	Format  Format
	Entries []Entry
	Issues  []Issue // only ever non-empty when Options.Lenient was set
}

var (
	zshHeaderRE = regexp.MustCompile(`^: (\d+):(\d+);(.*)$`)
	bashStampRE = regexp.MustCompile(`^#(\d+)$`)
)

// Parse reads a shell history file and returns its entries.
//
// In strict mode (the default), the first line that doesn't match the
// detected format aborts parsing and Parse returns a *ParseError. Pass
// Options{Lenient: true} to recover what can be recovered instead;
// anything tolerated is recorded in Result.Issues rather than raised.
func Parse(r io.Reader, opts Options) (*Result, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, err
	}

	format := opts.Format
	if format == FormatUnknown {
		format = detectFormat(lines)
	}

	switch format {
	case FormatZshExtended:
		return parseZshExtended(lines, opts.Lenient)
	case FormatBashTimestamped:
		return parseBashTimestamped(lines, opts.Lenient)
	default:
		return parsePlain(lines, opts.Lenient)
	}
}

func readLines(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	// commands can be long (heredocs, base64 blobs pasted into a shell);
	// the default 64KiB token limit is too easy to hit in practice.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("histlint: reading input: %w", err)
	}
	return lines, nil
}

// detectFormat looks at the first non-blank line only: a real history
// file is written by one shell in one format, so a file that mixes
// formats partway through is corrupt rather than ambiguous.
func detectFormat(lines []string) Format {
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		switch {
		case zshHeaderRE.MatchString(l):
			return FormatZshExtended
		case bashStampRE.MatchString(l):
			return FormatBashTimestamped
		default:
			return FormatPlain
		}
	}
	return FormatPlain
}

func parseZshExtended(lines []string, lenient bool) (*Result, error) {
	res := &Result{Format: FormatZshExtended}

	var cur *Entry // entry still accumulating continuation lines, if any

	for i, l := range lines {
		lineNo := i + 1

		if m := zshHeaderRE.FindStringSubmatch(l); m != nil {
			if cur != nil {
				issue := Issue{Line: lineNo, Message: "previous entry ends with a trailing backslash but is never continued"}
				if !lenient {
					return nil, &ParseError{issue}
				}
				res.Issues = append(res.Issues, issue)
				res.Entries = append(res.Entries, *cur)
				cur = nil
			}

			ts, _ := strconv.ParseInt(m[1], 10, 64)
			elapsed, _ := strconv.ParseInt(m[2], 10, 64)
			e := Entry{
				Timestamp: time.Unix(ts, 0),
				Elapsed:   time.Duration(elapsed) * time.Second,
				Line:      lineNo,
			}
			cmd := m[3]
			if strings.HasSuffix(cmd, `\`) {
				e.Command = strings.TrimSuffix(cmd, `\`)
				cur = &e
			} else {
				e.Command = cmd
				res.Entries = append(res.Entries, e)
			}
			continue
		}

		if cur == nil {
			issue := Issue{Line: lineNo, Message: "line does not start a new entry and none is open"}
			if !lenient {
				return nil, &ParseError{issue}
			}
			res.Issues = append(res.Issues, issue)
			continue
		}

		if strings.HasSuffix(l, `\`) {
			cur.Command += "\n" + strings.TrimSuffix(l, `\`)
			continue
		}
		cur.Command += "\n" + l
		res.Entries = append(res.Entries, *cur)
		cur = nil
	}

	if cur != nil {
		issue := Issue{Line: len(lines), Message: "unterminated multi-line entry at end of file"}
		if !lenient {
			return nil, &ParseError{issue}
		}
		res.Issues = append(res.Issues, issue)
		res.Entries = append(res.Entries, *cur)
	}

	return res, nil
}

func parseBashTimestamped(lines []string, lenient bool) (*Result, error) {
	res := &Result{Format: FormatBashTimestamped}

	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		l := lines[i]

		if strings.TrimSpace(l) == "" {
			continue
		}

		m := bashStampRE.FindStringSubmatch(l)
		if m == nil {
			issue := Issue{Line: lineNo, Message: "expected a #<epoch> timestamp line"}
			if !lenient {
				return nil, &ParseError{issue}
			}
			res.Issues = append(res.Issues, issue)
			res.Entries = append(res.Entries, Entry{Command: l, Line: lineNo})
			continue
		}

		if i+1 >= len(lines) || bashStampRE.MatchString(lines[i+1]) {
			issue := Issue{Line: lineNo, Message: "timestamp has no following command"}
			if !lenient {
				return nil, &ParseError{issue}
			}
			res.Issues = append(res.Issues, issue)
			continue
		}

		sec, _ := strconv.ParseInt(m[1], 10, 64)
		res.Entries = append(res.Entries, Entry{
			Command:   lines[i+1],
			Timestamp: time.Unix(sec, 0),
			Line:      lineNo,
		})
		i++ // consumed the command line too
	}

	return res, nil
}

// Dedup returns entries with exact duplicate commands collapsed to their
// most recent occurrence: earlier entries with the same Command text are
// dropped, and the surviving entry stays at its original position rather
// than moving to the end. This mirrors how a shell with erasedups-style
// history control ends up looking, rather than a plain "first seen" unique.
func Dedup(entries []Entry) []Entry {
	lastIndex := make(map[string]int, len(entries))
	for i, e := range entries {
		lastIndex[e.Command] = i
	}

	out := make([]Entry, 0, len(lastIndex))
	for i, e := range entries {
		if lastIndex[e.Command] == i {
			out = append(out, e)
		}
	}
	return out
}

// FilterByTime returns entries whose Timestamp falls in [since, until). A
// zero since or until leaves that side of the range unbounded, and a zero
// value for both returns entries unchanged. Entries with no Timestamp
// (plain-format history has none) are dropped whenever either bound is
// set, since there's nothing to compare them against.
func FilterByTime(entries []Entry, since, until time.Time) []Entry {
	if since.IsZero() && until.IsZero() {
		return entries
	}

	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Timestamp.IsZero() {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && !e.Timestamp.Before(until) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// CommandStat is one line of a usage summary: a command name and how many
// times it occurs.
type CommandStat struct {
	Command string
	Count   int
}

// Stats summarizes entries by the first whitespace-separated token of each
// command (its program name, e.g. "git" for "git status --short"), sorted
// by descending count and then alphabetically to break ties. A multi-line
// command is counted under the token that starts its first line, since
// that's the program that actually ran.
func Stats(entries []Entry) []CommandStat {
	counts := make(map[string]int)
	for _, e := range entries {
		name := e.Command
		if i := strings.IndexAny(name, " \t\n"); i >= 0 {
			name = name[:i]
		}
		if name == "" {
			continue
		}
		counts[name]++
	}

	out := make([]CommandStat, 0, len(counts))
	for name, n := range counts {
		out = append(out, CommandStat{Command: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Command < out[j].Command
	})
	return out
}

func parsePlain(lines []string, lenient bool) (*Result, error) {
	res := &Result{Format: FormatPlain}

	for i, l := range lines {
		lineNo := i + 1
		if strings.TrimSpace(l) == "" {
			issue := Issue{Line: lineNo, Message: "blank line in plain history"}
			if !lenient {
				return nil, &ParseError{issue}
			}
			res.Issues = append(res.Issues, issue)
			continue
		}
		res.Entries = append(res.Entries, Entry{Command: l, Line: lineNo})
	}

	return res, nil
}
