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

func TestEncodeErrors(t *testing.T) {
	for name, cfg := range map[string]Config{
		"empty payload": {Host: "127.0.0.1", Port: 6379, Payload: "\n  \n"},
		"blank host":    {Host: "", Port: 6379, Payload: "PING"},
		"bad host":      {Host: "redis:6379", Port: 6379, Payload: "PING"},
		"bad port":      {Host: "127.0.0.1", Port: 99999, Payload: "PING"},
		"zero port":     {Host: "127.0.0.1", Port: 0, Payload: "PING"},
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
