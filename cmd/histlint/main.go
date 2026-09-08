// Command histlint parses a shell history file and reports what it finds:
// the detected format, how many entries it holds, and any lines that
// don't fit that format.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jenniferr90/histlint"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dedup":
			runDedup(os.Args[2:])
			return
		case "search":
			runSearch(os.Args[2:])
			return
		}
	}
	runReport(os.Args[1:])
}

func runReport(args []string) {
	fs := flag.NewFlagSet("histlint", flag.ExitOnError)
	lenient := fs.Bool("lenient", false, "tolerate malformed lines instead of stopping on the first one")
	format := fs.String("format", "auto", "history format: auto, plain, bash, zsh")
	asJSON := fs.Bool("json", false, "print entries as JSON lines instead of a summary")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: histlint [flags] [file]\n\n"+
			"Reads a shell history file and reports its format, entry count,\n"+
			"and any lines that don't fit that format. Fails on the first\n"+
			"malformed line unless --lenient is given.\n\n"+
			"With no file argument, reads piped stdin if there is any,\n"+
			"otherwise falls back to $HISTFILE or a default path guessed\n"+
			"from $SHELL.\n\n"+
			"Subcommands:\n"+
			"  dedup    print each command once, dropping earlier duplicates\n"+
			"  search   filter commands by text and/or time range\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	f, err := resolveFormat(*format)
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		os.Exit(2)
	}

	in, err := openInput(fs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		os.Exit(1)
	}
	defer in.Close()

	res, err := histlint.Parse(in, histlint.Options{Lenient: *lenient, Format: f})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if !*lenient {
			fmt.Fprintln(os.Stderr, "histlint: rerun with --lenient to skip bad lines and continue")
		}
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		for _, e := range res.Entries {
			if err := enc.Encode(e); err != nil {
				fmt.Fprintln(os.Stderr, "histlint:", err)
				os.Exit(1)
			}
		}
		return
	}

	fmt.Printf("format:  %s\n", res.Format)
	fmt.Printf("entries: %d\n", len(res.Entries))
	if len(res.Issues) > 0 {
		fmt.Printf("issues:  %d\n", len(res.Issues))
		for _, iss := range res.Issues {
			fmt.Printf("  %s\n", iss)
		}
	}
}

func runDedup(args []string) {
	fs := flag.NewFlagSet("dedup", flag.ExitOnError)
	lenient := fs.Bool("lenient", false, "tolerate malformed lines instead of stopping on the first one")
	format := fs.String("format", "auto", "history format: auto, plain, bash, zsh")
	asJSON := fs.Bool("json", false, "print deduped entries as JSON lines instead of one command per line")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: histlint dedup [flags] [file]\n\n"+
			"Reads a shell history file and prints each distinct command\n"+
			"once, keeping its most recent occurrence and dropping earlier\n"+
			"duplicates. With no file argument, reads piped stdin if there\n"+
			"is any, otherwise falls back to $HISTFILE or a default path\n"+
			"guessed from $SHELL.\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	f, err := resolveFormat(*format)
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		os.Exit(2)
	}

	in, err := openInput(fs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		os.Exit(1)
	}
	defer in.Close()

	res, err := histlint.Parse(in, histlint.Options{Lenient: *lenient, Format: f})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if !*lenient {
			fmt.Fprintln(os.Stderr, "histlint: rerun with --lenient to skip bad lines and continue")
		}
		os.Exit(1)
	}

	deduped := histlint.Dedup(res.Entries)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		for _, e := range deduped {
			if err := enc.Encode(e); err != nil {
				fmt.Fprintln(os.Stderr, "histlint:", err)
				os.Exit(1)
			}
		}
		return
	}

	for _, e := range deduped {
		fmt.Println(e.Command)
	}
	fmt.Fprintf(os.Stderr, "histlint: %d entries, %d unique, %d duplicates removed\n",
		len(res.Entries), len(deduped), len(res.Entries)-len(deduped))
}

func runSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	lenient := fs.Bool("lenient", false, "tolerate malformed lines instead of stopping on the first one")
	format := fs.String("format", "auto", "history format: auto, plain, bash, zsh")
	asJSON := fs.Bool("json", false, "print matching entries as JSON lines instead of one command per line")
	query := fs.String("query", "", "only include commands containing this substring (case-insensitive)")
	since := fs.String("since", "", "only include entries at or after this time")
	until := fs.String("until", "", "only include entries before this time")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: histlint search [flags] [file]\n\n"+
			"Reads a shell history file and prints commands matching\n"+
			"--query and/or falling within [--since, --until). Times accept\n"+
			"RFC3339, \"2006-01-02 15:04:05\", or \"2006-01-02\". Plain history\n"+
			"has no timestamps, so --since/--until only apply to bash-timestamped\n"+
			"or zsh-extended input. With no file argument, reads piped stdin\n"+
			"if there is any, otherwise falls back to $HISTFILE or a default\n"+
			"path guessed from $SHELL.\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	f, err := resolveFormat(*format)
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		os.Exit(2)
	}

	var sinceTime, untilTime time.Time
	if *since != "" {
		if sinceTime, err = parseTimeArg(*since); err != nil {
			fmt.Fprintln(os.Stderr, "histlint:", err)
			os.Exit(2)
		}
	}
	if *until != "" {
		if untilTime, err = parseTimeArg(*until); err != nil {
			fmt.Fprintln(os.Stderr, "histlint:", err)
			os.Exit(2)
		}
	}

	in, err := openInput(fs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		os.Exit(1)
	}
	defer in.Close()

	res, err := histlint.Parse(in, histlint.Options{Lenient: *lenient, Format: f})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if !*lenient {
			fmt.Fprintln(os.Stderr, "histlint: rerun with --lenient to skip bad lines and continue")
		}
		os.Exit(1)
	}

	if (!sinceTime.IsZero() || !untilTime.IsZero()) && res.Format == histlint.FormatPlain {
		fmt.Fprintln(os.Stderr, "histlint: plain history has no timestamps; --since/--until will match nothing")
	}

	matched := histlint.FilterByTime(res.Entries, sinceTime, untilTime)
	if *query != "" {
		var filtered []histlint.Entry
		q := strings.ToLower(*query)
		for _, e := range matched {
			if strings.Contains(strings.ToLower(e.Command), q) {
				filtered = append(filtered, e)
			}
		}
		matched = filtered
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		for _, e := range matched {
			if err := enc.Encode(e); err != nil {
				fmt.Fprintln(os.Stderr, "histlint:", err)
				os.Exit(1)
			}
		}
		return
	}

	for _, e := range matched {
		fmt.Println(e.Command)
	}
}

// openInput picks the history file to read for a subcommand: an explicit
// file argument wins, then piped stdin, then $HISTFILE or a shell-guessed
// default. Falling back to a real history file only when stdin is a
// terminal keeps `histlint <flags> < file` and pipelines working exactly
// as before this fallback was added.
func openInput(fs *flag.FlagSet) (io.ReadCloser, error) {
	if fs.NArg() > 0 {
		return os.Open(fs.Arg(0))
	}
	if !isTerminal(os.Stdin) {
		return io.NopCloser(os.Stdin), nil
	}
	path, err := defaultHistFile()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("no file given and stdin isn't piped, so falling back to %s: %w", path, err)
	}
	return f, nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// defaultHistFile locates a history file to fall back to when no file
// argument is given and stdin isn't piped: $HISTFILE if set, otherwise a
// guess based on $SHELL, since that's what most people mean by "my
// history" even though bash and zsh don't agree on where it lives.
func defaultHistFile() (string, error) {
	if h := os.Getenv("HISTFILE"); h != "" {
		return h, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no file given, stdin isn't piped, and $HISTFILE is unset: %w", err)
	}

	switch shell := filepath.Base(os.Getenv("SHELL")); shell {
	case "zsh":
		return filepath.Join(home, ".zsh_history"), nil
	case "bash":
		return filepath.Join(home, ".bash_history"), nil
	default:
		return "", fmt.Errorf("no file given, stdin isn't piped, $HISTFILE is unset, and $SHELL (%q) isn't bash or zsh", os.Getenv("SHELL"))
	}
}

var searchTimeLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseTimeArg accepts RFC3339 (with an explicit zone) or one of a few
// common zone-less layouts, which are interpreted in the local zone.
func parseTimeArg(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range searchTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q (want RFC3339, \"2006-01-02 15:04:05\", or \"2006-01-02\")", s)
}

func resolveFormat(s string) (histlint.Format, error) {
	switch s {
	case "auto":
		return histlint.FormatUnknown, nil
	case "plain":
		return histlint.FormatPlain, nil
	case "bash":
		return histlint.FormatBashTimestamped, nil
	case "zsh":
		return histlint.FormatZshExtended, nil
	default:
		return histlint.FormatUnknown, fmt.Errorf("unknown format %q (want auto, plain, bash, or zsh)", s)
	}
}
