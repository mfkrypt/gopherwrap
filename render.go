package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles are immutable lipgloss values (methods return copies), so keeping
// them at package scope is state-free. label is 9 cells wide so the panel
// columns line up — lipgloss pads by visible width, not by escape bytes.
var (
	accent  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8a5a00", Dark: "#ffb000"})
	urlStyl = lipgloss.NewStyle().Bold(true)
	label   = accent.Bold(true).Width(9)
	dim     = lipgloss.NewStyle().Faint(true)
)

// plural appends s to a label when n != 1. Pure word helper.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// rule renders a full-width-ish dashed heading: ── title ───────…
func rule(title string) string {
	const width = 64
	title = " " + title + " "
	fill := strings.Repeat("─", max(3, width-lipgloss.Width(title)))
	return accent.Render("─" + title + fill)
}

// renderResult renders one Result as a styled, copy-friendly block.
// Pure: given the same Result it always returns the same string.
func renderResult(res Result) string {
	var b strings.Builder
	b.WriteString("\n" + rule("GopherWrap — Redis → gopher://") + "\n\n")

	fmt.Fprintf(&b, "  %s %s\n", label.Render("target"), res.Host+":"+fmt.Sprint(res.Port))
	fmt.Fprintf(&b, "  %s %d %s\n", label.Render("commands"), len(res.Commands), plural(len(res.Commands), "command"))
	for i, c := range res.Commands {
		fmt.Fprintf(&b, "%12s%2d  %s\n", "", i+1, c)
	}
	fmt.Fprintf(&b, "  %s %s\n", label.Render("framing"), res.Framing)
	fmt.Fprintf(&b, "  %s %s\n\n", label.Render("wire"), dim.Render(escapeVisible(res.Wire)))

	b.WriteString(rule("Encoded Gopher URL") + "\n")
	fmt.Fprintf(&b, "  %s\n\n", urlStyl.Render(res.URL))

	b.WriteString(rule("x2 Encoded Gopher URL") + "\n")
	fmt.Fprintf(&b, "  %s\n", dim.Render(res.Double))

	return b.String()
}
