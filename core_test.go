package main

import (
	"strings"
	"testing"
)

func TestFormatHost(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "127.0.0.1", want: "127.0.0.1"},
		{in: "  redis.internal  ", want: "redis.internal"},
		{in: "redis.internal.example", want: "redis.internal.example"},
		{in: "::1", want: "[::1]"},
		{in: "[::1]", want: "[::1]"},
		{in: "fe80::1%eth0", wantErr: true}, // zones carry '%', rejected by the URL-safe charset
		{in: "", wantErr: true},
		{in: "http://redis.internal", wantErr: true},
		{in: "redis.internal:6379", wantErr: true},
		{in: "redis internal", wantErr: true},
		{in: "red\nis", wantErr: true},
		{in: "127.0.0.1/path", wantErr: true},
		{in: "100.200.300.400", want: "100.200.300.400"}, // syntactically a name; DNS decides
	}
	for _, tt := range tests {
		got, err := formatHost(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("formatHost(%q): expected error, got %q", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("formatHost(%q): unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("formatHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParsePort(t *testing.T) {
	if _, err := parsePort("6379"); err != nil {
		t.Errorf("parsePort(6379): unexpected error: %v", err)
	}
	if _, err := parsePort(" 6379 "); err != nil {
		t.Errorf("parsePort(\" 6379 \"): unexpected error: %v", err)
	}
	for _, bad := range []string{"", "abc", "0", "65536", "-1", "6379.5"} {
		if _, err := parsePort(bad); err == nil {
			t.Errorf("parsePort(%q): expected error", bad)
		}
	}
}

func TestSplitCommands(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{in: "SET test hello", want: []string{"SET test hello"}},
		{in: "SET test hello\nCONFIG GET dir\n", want: []string{"SET test hello", "CONFIG GET dir"}},
		{in: "AUTH secret\r\nSET x 1\r\n", want: []string{"AUTH secret", "SET x 1"}},
		{in: "\n  \nSET   spaced  args\n\t\n", want: []string{"SET   spaced  args"}},
		{in: "", want: nil},
		{in: "  \n\n ", want: nil},
	}
	for _, tt := range tests {
		got := splitCommands(tt.in)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("splitCommands(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestCommandsToWire(t *testing.T) {
	got := commandsToWire([]string{"SET test hello", "FLUSHALL"})
	want := "SET test hello\r\nFLUSHALL\r\n"
	if got != want {
		t.Errorf("commandsToWire = %q, want %q", got, want)
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{in: "SET test hello", want: []string{"SET", "test", "hello"}},
		{in: "  SET   spaced\targs  ", want: []string{"SET", "spaced", "args"}},
		{in: `SET key "hello world"`, want: []string{"SET", "key", "hello world"}},
		{in: `SET k 'single quoted'`, want: []string{"SET", "k", "single quoted"}},
		{in: `""`, want: []string{""}}, // empty quoted arg is a real $0 argument
		{in: `SET "" x`, want: []string{"SET", "", "x"}},
		{in: `key"a b"`, want: []string{"keya b"}},           // quotes toggle mid-word, like Redis
		{in: `SET a\b c`, want: []string{"SET", `a\b`, "c"}}, // bare backslash is literal
		// double quotes decode escapes to real bytes
		{in: "SET cron \"*/1 * * * * root /tmp/p.sh\\n\"", want: []string{"SET", "cron", "*/1 * * * * root /tmp/p.sh\n"}},
		{in: `"tab\there"`, want: []string{"tab\there"}},
		{in: `"back\\slash"`, want: []string{`back\slash`}},
		{in: `"say \"hi\""`, want: []string{`say "hi"`}},
		{in: `"A\x42"`, want: []string{"AB"}},
		{in: `"nul\x00byte"`, want: []string{"nul\x00byte"}},
		// single quotes group literally; \' is the only escape
		{in: `'it\'s'`, want: []string{"it's"}},
		{in: `'no\nescape'`, want: []string{`no\nescape`}},

		{in: `SET "unclosed`, wantErr: true},
		{in: `SET 'unclosed`, wantErr: true},
		{in: `SET "a\q"`, wantErr: true},   // unknown escape
		{in: `SET "a\x"`, wantErr: true},   // \x needs two hex digits
		{in: `SET "a\x4z"`, wantErr: true}, // second hex digit missing
		{in: `SET "a"b`, wantErr: true},    // closing quote must end the argument
		{in: `SET "a\`, wantErr: true},     // dangling backslash
	}
	for _, tt := range tests {
		got, err := splitArgs(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("splitArgs(%q): expected error, got %#v", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitArgs(%q): unexpected error: %v", tt.in, err)
			continue
		}
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("splitArgs(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestCommandsToRESP(t *testing.T) {
	tests := []struct {
		in      []string
		want    string
		wantErr bool
	}{
		{in: []string{"PING"}, want: "*1\r\n$4\r\nPING\r\n"},
		{in: []string{"SET k v", "PING"},
			want: "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n*1\r\n$4\r\nPING\r\n"},
		// the escaped \n decodes into a real LF byte inside the $27 bulk —
		// the payload inline framing could never carry
		{in: []string{`SET cron "*/1 * * * * root /tmp/p.sh\n"`},
			want: "*3\r\n$3\r\nSET\r\n$4\r\ncron\r\n$27\r\n*/1 * * * * root /tmp/p.sh\n\r\n"},
		// CRLF inside an argument is length-protected, not a frame break
		{in: []string{`SET x "a\r\nb"`},
			want: "*3\r\n$3\r\nSET\r\n$1\r\nx\r\n$4\r\na\r\nb\r\n"},
		{in: []string{`SET "unclosed`}, wantErr: true},
	}
	for _, tt := range tests {
		got, err := commandsToRESP(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("commandsToRESP(%q): expected error, got %q", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("commandsToRESP(%q): unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("commandsToRESP(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPercentEncode(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "SET test hello\r\n", want: "SET%20test%20hello%0D%0A"},
		{in: "CONFIG SET dir /tmp", want: "CONFIG%20SET%20dir%20%2Ftmp"},
		{in: "EVAL \"return 1\" 0", want: "EVAL%20%22return%201%22%200"},
		{in: "SET café 1", want: "SET%20caf%C3%A9%201"}, // bytes, not runes — URL-safe either way
		{in: "100%", want: "100%25"},
		{in: "AZaz09", want: "AZaz09"},
	}
	for _, tt := range tests {
		if got := percentEncode(tt.in); got != tt.want {
			t.Errorf("percentEncode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAssembleURL(t *testing.T) {
	if got := assembleURL("127.0.0.1", 6379, "INFO%0D%0A"); got != "gopher://127.0.0.1:6379/_INFO%0D%0A" {
		t.Errorf("assembleURL ipv4 = %q", got)
	}
	if got := assembleURL("[::1]", 6379, "x"); got != "gopher://[::1]:6379/_x" {
		t.Errorf("assembleURL ipv6 = %q", got)
	}
}

func TestDoubleEncode(t *testing.T) {
	got := doubleEncode("gopher://127.0.0.1:6379/_SET%20x%0D%0A")
	// Every byte percent-encoded once more (dots included): one app-side
	// decode restores the single-encoded gopher URL.
	want := "gopher%3A%2F%2F127%2E0%2E0%2E1%3A6379%2F%5FSET%2520x%250D%250A"
	if got != want {
		t.Errorf("doubleEncode = %q, want %q", got, want)
	}
}

func TestEncodeGolden(t *testing.T) {
	res, err := encode(Default())
	if err != nil {
		t.Fatalf("encode(default): %v", err)
	}
	wantURL := "gopher://127.0.0.1:6379/_SET%20test%20hello%0D%0A"
	if res.URL != wantURL {
		t.Errorf("encode(default).URL = %q, want %q", res.URL, wantURL)
	}
	if res.Wire != "SET test hello\r\n" {
		t.Errorf("encode(default).Wire = %q", res.Wire)
	}
	if res.Host != "127.0.0.1" || res.Port != 6379 {
		t.Errorf("encode(default).Host/Port = %q/%d", res.Host, res.Port)
	}
	if res.Framing != "inline" {
		t.Errorf("encode(default).Framing = %q, want inline (back-compat)", res.Framing)
	}

	multi := Config{Host: "::1", Port: 6380, Payload: "AUTH hunter2\nCONFIG SET dir /var/spool/cron\n"}
	res, err = encode(multi)
	if err != nil {
		t.Fatalf("encode(multi): %v", err)
	}
	wantWire := "AUTH hunter2\r\nCONFIG SET dir /var/spool/cron\r\n"
	wantURL = "gopher://[::1]:6380/_AUTH%20hunter2%0D%0ACONFIG%20SET%20dir%20%2Fvar%2Fspool%2Fcron%0D%0A"
	if res.Wire != wantWire {
		t.Errorf("encode(multi).Wire = %q, want %q", res.Wire, wantWire)
	}
	if res.URL != wantURL {
		t.Errorf("encode(multi).URL = %q, want %q", res.URL, wantURL)
	}
	if res.Double != doubleEncode(wantURL) {
		t.Errorf("encode(multi).Double is not the double-encoding of URL")
	}
	if len(res.Commands) != 2 || res.Commands[0] != "AUTH hunter2" {
		t.Errorf("encode(multi).Commands = %#v", res.Commands)
	}
}

func TestEncodeRESP(t *testing.T) {
	res, err := encode(Config{Host: "127.0.0.1", Port: 6379,
		Payload: "SET cron \"*/1 * * * * root /tmp/p.sh\\n\"", Resp: true})
	if err != nil {
		t.Fatalf("encode(resp): %v", err)
	}
	wantWire := "*3\r\n$3\r\nSET\r\n$4\r\ncron\r\n$27\r\n*/1 * * * * root /tmp/p.sh\n\r\n"
	if res.Wire != wantWire {
		t.Errorf("encode(resp).Wire = %q, want %q", res.Wire, wantWire)
	}
	if res.Framing != "RESP" {
		t.Errorf("encode(resp).Framing = %q, want RESP", res.Framing)
	}
	if len(res.Commands) != 1 {
		t.Errorf("encode(resp).Commands = %#v", res.Commands)
	}
	// The URL is the frame's bytes percent-encoded after the underscore;
	// percentEncode itself is golden-tested above, so composing keeps the
	// expectation readable while still pinning the full pipeline.
	wantURL := "gopher://127.0.0.1:6379/_" + percentEncode(wantWire)
	if res.URL != wantURL {
		t.Errorf("encode(resp).URL = %q, want %q", res.URL, wantURL)
	}
	if res.Double != doubleEncode(wantURL) {
		t.Errorf("encode(resp).Double is not the double-encoding of URL")
	}
}

func TestEncodeErrors(t *testing.T) {
	for name, cfg := range map[string]Config{
		"empty payload":      {Host: "127.0.0.1", Port: 6379, Payload: "\n  \n"},
		"blank host":         {Host: "", Port: 6379, Payload: "PING"},
		"bad host":           {Host: "redis:6379", Port: 6379, Payload: "PING"},
		"bad port":           {Host: "127.0.0.1", Port: 99999, Payload: "PING"},
		"zero port":          {Host: "127.0.0.1", Port: 0, Payload: "PING"},
		"resp bad quotes":    {Host: "127.0.0.1", Port: 6379, Payload: `SET "oops`, Resp: true},
		"resp bad escape":    {Host: "127.0.0.1", Port: 6379, Payload: `SET x "\q"`, Resp: true},
		"resp multi cmd err": {Host: "127.0.0.1", Port: 6379, Payload: "PING\nSET x 'no", Resp: true},
	} {
		if _, err := encode(cfg); err == nil {
			t.Errorf("encode(%s): expected error", name)
		}
	}
}

func TestEscapeVisible(t *testing.T) {
	if got := escapeVisible("SET x 1\r\nCONFIG GET dir\r\n"); got != "SET x 1\\r\\nCONFIG GET dir\\r\\n" {
		t.Errorf("escapeVisible = %q", got)
	}
}

func TestWithPayload(t *testing.T) {
	cfg := Default().withPayload("PING")
	if cfg.Payload != "PING" || cfg.Host != "127.0.0.1" || cfg.Port != 6379 {
		t.Errorf("withPayload changed other fields: %#v", cfg)
	}
}
