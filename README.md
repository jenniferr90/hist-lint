# histlint

A parser and CLI for shell history files (`~/.bash_history`, `~/.zsh_history`).

## Why

History files look like plain text but aren't quite: zsh's extended
history format prefixes every entry with a timestamp header and escapes
embedded newlines with a trailing backslash; bash with `HISTTIMEFORMAT`
set writes a `#<epoch>` line before each command; plain bash just writes
commands one after another, including any embedded newlines from
multi-line commands, with nothing marking where one entry ends and the
next begins.

Tools that treat these files as "one command per line" get it wrong as
soon as they hit a multi-line command, a file that got concatenated from
two different shells, or a truncated write from a crashed session. The
mistake is usually silent: you get a plausible-looking but wrong split.

histlint parses strictly by default. If a line doesn't fit the format
detected from the rest of the file, `Parse` stops and reports exactly
where. Pass `--lenient` (or `Options.Lenient` in the library) to recover
what can be recovered instead, with every skipped or patched-up line
recorded so you know what was tolerated.

## Install

```
go install github.com/jenniferr90/histlint/cmd/histlint@latest
```

Or build from a clone:

```
go build -o bin/histlint ./cmd/histlint
```

## CLI usage

```
$ histlint ~/.zsh_history
format:  zsh-extended
entries: 4213

$ histlint --lenient ~/.zsh_history
format:  zsh-extended
entries: 4211
issues:  2
  line 1882: previous entry ends with a trailing backslash but is never continued
  line 4213: unterminated multi-line entry at end of file

$ histlint --json ~/.bash_history | jq -r '.Command' | grep -c ssh
17
```

By default a malformed line is a hard error and the CLI exits 1:

```
$ histlint ./corrupted_history
histlint: line 118: line does not start a new entry and none is open
histlint: rerun with --lenient to skip bad lines and continue
```

## Library usage

```go
package main

import (
	"fmt"
	"os"

	"github.com/jenniferr90/histlint"
)

func main() {
	f, err := os.Open("testdata/zsh_history")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	res, err := histlint.Parse(f, histlint.Options{Lenient: true})
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s: %d entries, %d issues\n", res.Format, len(res.Entries), len(res.Issues))
	for _, e := range res.Entries {
		if !e.Timestamp.IsZero() {
			fmt.Println(e.Timestamp.Format("2006-01-02 15:04:05"), e.Command)
		}
	}
}
```

## Supported formats

- `plain` — one command per line, no metadata (default bash without
  `HISTTIMEFORMAT`)
- `bash-timestamped` — `#<epoch>` line followed by a command line
- `zsh-extended` — `: <epoch>:<elapsed>;<command>`, with multi-line
  commands escaped across lines using a trailing backslash

Format is auto-detected from the first non-blank line unless overridden
with `--format` (CLI) or `Options.Format` (library).

## Status

Early. Parsing and the CLI summary/JSON output work; nothing here reads
history *out of* a live shell session yet, and there's no dedup or
search tooling on top. See the issue tracker for what's next.

## License

MIT, see [LICENSE](LICENSE).
