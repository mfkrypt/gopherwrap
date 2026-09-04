package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"
)

// session carries the raw form state between rounds of the interactive
// loop. It is an immutable value: every round produces the next session,
// nothing is mutated.
type session struct {
	host    string
	port    string
	payload string
	resp    bool // frame commands as RESP arrays (default: inline text)
}

// newSession returns the first-round defaults.
func newSession() session {
	return session{host: "127.0.0.1", port: "6379", payload: "SET test hello", resp: false}
}

// toConfig parses the session into a validated Config.
func (s session) toConfig() (Config, error) {
	port, err := parsePort(s.port)
	if err != nil {
		return Config{}, err
	}
	return Config{Host: s.host, Port: port, Payload: s.payload, Resp: s.resp}, nil
}

// runInteractive drives the loop: form → encode → render → ask again.
// The previous round's answers become the next round's defaults, so
// iterating on a payload (host/port fixed, command tweaked) is a
// form-Enter-Enter cadence.
func runInteractive(out, errOut io.Writer) error {
	for s := newSession(); ; {
		next, err := promptConfig(s)
		if err != nil {
			return err
		}
		if res, err := encode(mustConfig(next)); err != nil {
			fmt.Fprintf(errOut, "error: %v\n", err)
		} else {
			fmt.Fprint(out, renderResult(res))
		}
		again, err := promptAgain()
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
		s = next
	}
}

// mustConfig converts a session validated by the form; a failure here is a
// programming error (the form validators mirror the core validators).
func mustConfig(s session) Config {
	cfg, err := s.toConfig()
	if err != nil {
		panic("gopherwrap: form accepted an invalid session: " + err.Error())
	}
	return cfg
}

// promptConfig shows the main form. The fields mirror the core validators
// so invalid input is rejected inline, at the field, before encoding.
func promptConfig(prev session) (session, error) {
	host, port, payload, resp := prev.host, prev.port, prev.payload, prev.resp

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Host").
				Description("Redis the SSRF point can reach").
				Value(&host).
				Validate(func(v string) error {
					_, err := formatHost(v)
					return err
				}),
			huh.NewInput().
				Title("Port").
				Description("Redis listens here by default").
				Value(&port).
				Validate(func(v string) error {
					_, err := parsePort(v)
					return err
				}),
			// Framing precedes Payload on purpose: huh validates fields as
			// the user advances, and the payload validator below needs the
			// final mode to know whether to tokenize.
			huh.NewConfirm().
				Title("Framing").
				Description("Inline: CRLF per command, Redis splits args. RESP: multi-bulk arrays, length-prefixed args (\\n, \\xHH escapes decode inside quotes).").
				Affirmative("RESP arrays").
				Negative("Inline text").
				Value(&resp),
			huh.NewText().
				Title("Payload").
				Lines(4).
				CharLimit(0).
				ExternalEditor(false).
				Description("Alt+Enter = new line · Enter = encode").
				Value(&payload).
				Validate(func(v string) error {
					cmds := splitCommands(v)
					if len(cmds) == 0 {
						return errors.New("payload needs at least one Redis command")
					}
					if resp {
						if _, err := commandsToRESP(cmds); err != nil {
							return err
						}
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeCharm()).Run()
	if err != nil {
		return session{}, err
	}
	return session{host: host, port: port, payload: payload, resp: resp}, nil
}

// promptAgain asks whether to run another round (default yes — iterating
// payloads is the common case).
//
// q (or Q) quits instantly. It is only bound on this prompt, never on the
// payload form: there q is a normal character a user might legitimately
// type ("SET queue x"), and huh matches quit keys before fields ever see
// them, so a global q-quit would abort mid-typing.
func promptAgain() (bool, error) {
	again := true
	km := huh.NewDefaultKeyMap()
	km.Quit.SetKeys("q", "Q", "ctrl+c")
	km.Quit.SetHelp("q", "quit")
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Encode another payload?").
				Affirmative("Run again").
				Negative("Quit").
				Value(&again),
		),
	).WithKeyMap(km).Run()
	if err != nil {
		return false, err
	}
	return again, nil
}
