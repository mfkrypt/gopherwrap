// GopherWrap is an interactive helper that encodes Redis commands into a
// gopher:// URL for SSRF testing. CRLF terminators are appended to every
// command automatically.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

const usageText = `GopherWrap — Redis commands → gopher:// URL (SSRF testing).

Modes:
  gopherwrap                  interactive form (Host / Port / Payload)
  gopherwrap -f cmds.txt      encode a payload file, print the URL
  echo 'SET x 1' | gopherwrap read the payload from stdin

Every payload line is one Redis command; CRLF is appended automatically.
The gopher URL is printed alone on stdout so it can be piped to other
tools. Add -d to also print the double-encoded variant.

Flags:`

type options struct {
	host    string
	port    int
	payload string
	file    string
	double  bool
	seen    map[string]bool // flags set explicitly on the command line
}

func main() {
	opts := parseFlags(os.Args[1:])
	if err := run(opts, os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return // ctrl+c / esc — quiet exit
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// parseFlags wires the CLI. Flags are only defaults for the interactive
// form; the presence of any explicit flag selects non-interactive mode.
func parseFlags(args []string) options {
	fs := flag.NewFlagSet("gopherwrap", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usageText)
		fs.PrintDefaults()
	}
	opts := options{seen: make(map[string]bool)}
	fs.StringVar(&opts.host, "host", "127.0.0.1", "redis host for the gopher URL")
	fs.IntVar(&opts.port, "port", 6379, "redis port for the gopher URL")
	fs.StringVar(&opts.payload, "payload", "", "redis commands, one per line (\\n or newlines separate them)")
	fs.StringVar(&opts.file, "file", "", "read the payload from this file")
	fs.StringVar(&opts.file, "f", "", "alias for -file")
	fs.BoolVar(&opts.double, "d", false, "also print the double-encoded variant")
	if err := fs.Parse(args); err != nil {
		os.Exit(2) // flag pkg already printed the reason + usage
	}
	fs.Visit(func(f *flag.Flag) { opts.seen[f.Name] = true })
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		os.Exit(2)
	}
	return opts
}

// run selects the mode: explicit flags → one-shot CLI, terminal stdin →
// interactive form, otherwise a piped payload with default target.
func run(opts options, stdin io.Reader, stdout, stderr io.Writer) error {
	switch {
	case len(opts.seen) > 0:
		payload, err := payloadFor(opts, stdin, stderr)
		if err != nil {
			return err
		}
		return encodeAndPrint(Config{Host: opts.host, Port: opts.port, Payload: payload}, opts.double, stdout)
	case isTerminal(stdin):
		return runInteractive(stdout, stderr)
	default:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		if len(splitCommands(string(data))) == 0 {
			return errors.New("no payload: pass -payload, -file or pipe commands to stdin")
		}
		return encodeAndPrint(Default().withPayload(string(data)), false, stdout)
	}
}

// payloadFor resolves the non-interactive payload source: flag, file or
// piped stdin, in that order.
func payloadFor(opts options, stdin io.Reader, stderr io.Writer) (string, error) {
	if opts.payload != "" && opts.file != "" {
		return "", errors.New("-payload and -file are mutually exclusive")
	}
	switch {
	case opts.payload != "":
		return opts.payload, nil
	case opts.file != "":
		data, err := os.ReadFile(opts.file)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", opts.file, err)
		}
		return string(data), nil
	case !isTerminal(stdin):
		fmt.Fprintln(stderr, "note: reading payload from stdin")
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}
	return "", errors.New("no payload: pass -payload or -file (stdin is a terminal)")
}

// encodeAndPrint runs the pure pipeline and prints the URL(s) — the gopher
// URL alone on stdout unless double is set, which appends the re-encoded
// variant on the next line.
func encodeAndPrint(cfg Config, double bool, stdout io.Writer) error {
	res, err := encode(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, res.URL)
	if double {
		fmt.Fprintln(stdout, res.Double)
	}
	return nil
}

// withPayload swaps the payload of a Config, keeping its other defaults.
func (c Config) withPayload(payload string) Config {
	c.Payload = payload
	return c
}

// isTerminal reports whether r (an *os.File at the call sites) is attached
// to a terminal.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
