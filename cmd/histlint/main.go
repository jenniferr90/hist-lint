// Command histlint parses a shell history file and reports what it finds:
// the detected format, how many entries it holds, and any lines that
// don't fit that format.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jenniferr90/histlint"
)

func main() {
	var (
		lenient = flag.Bool("lenient", false, "tolerate malformed lines instead of stopping on the first one")
		format  = flag.String("format", "auto", "history format: auto, plain, bash, zsh")
		asJSON  = flag.Bool("json", false, "print entries as JSON lines instead of a summary")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: histlint [flags] [file]\n\n"+
			"Reads a shell history file (or stdin) and reports its format,\n"+
			"entry count, and any lines that don't fit that format. Fails\n"+
			"on the first malformed line unless --lenient is given.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	f, err := resolveFormat(*format)
	if err != nil {
		fmt.Fprintln(os.Stderr, "histlint:", err)
		os.Exit(2)
	}

	in := os.Stdin
	if flag.NArg() > 0 {
		file, err := os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "histlint:", err)
			os.Exit(1)
		}
		defer file.Close()
		in = file
	}

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
