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
}

// newSession returns the first-round defaults.
func newSession() session {
	return session{host: "127.0.0.1", port: "6379", payload: "SET test hello"}
}

// toConfig parses the session into a validated Config.
func (s session) toConfig() (Config, error) {
	port, err := parsePort(s.port)
	if err != nil {
		return Config{}, err
	}
	return Config{Host: s.host, Port: port, Payload: s.payload}, nil
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
	host, port, payload := prev.host, prev.port, prev.payload

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
			huh.NewText().
				Title("Payload").
				Lines(4).
				CharLimit(0).
				Description("One Redis command per line — CRLF is added automatically · alt+enter = new line · ctrl+e = $EDITOR · enter = encode").
				Value(&payload).
				Validate(func(v string) error {
					if len(splitCommands(v)) == 0 {
						return errors.New("payload needs at least one Redis command")
					}
					return nil
				}),
		).Title("GopherWrap — Redis over gopher:// (SSRF)"),
	).WithTheme(huh.ThemeCharm()).Run()
	if err != nil {
		return session{}, err
	}
	return session{host: host, port: port, payload: payload}, nil
}

// promptAgain asks whether to run another round (default yes — iterating
// payloads is the common case).
func promptAgain() (bool, error) {
	again := true
	err := huh.NewConfirm().
		Title("Encode another payload?").
		Affirmative("Run again").
		Negative("Quit").
		Value(&again).
		Run()
	if err != nil {
		return false, err
	}
	return again, nil
}
