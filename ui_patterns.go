package main

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// The reference patterns and pinned Crush source links are in docs/ui-patterns.md.
func workshopRenderer(width int) *glamour.TermRenderer {
	style := styles.DarkStyleConfig
	text, link, heading, code, bg := "#FFF5FC", "#69DDF4", "#FF4FA3", "#37E6B5", "#36215A"
	margin := uint(0)
	style.Document.Color = &text
	style.Document.Margin = &margin
	style.Heading.Color = &heading
	style.Link.Color = &link
	style.LinkText.Color = &link
	style.Code.Color = &code
	style.Code.BackgroundColor = &bg
	style.CodeBlock.Theme = "dracula"
	renderer, _ := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(max(20, width)))
	return renderer
}

func gradient(text string) string {
	var b strings.Builder
	runes := []rune(text)
	// One stable pink-to-violet ramp, independent of frames and window size.
	for i, r := range runes {
		f := float64(i) / float64(max(1, len(runes)-1))
		c := color.RGBA{uint8(255 - 102*f), uint8(79 + 39*f), uint8(163 + 92*f), 255}
		b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
	}
	return b.String()
}

func (m model) workshopSidebar() bool { return m.width >= 120 && m.height >= 32 }

func (m model) workshopWidth() int {
	if m.workshopSidebar() {
		return m.width - 35
	}
	return m.width - 6
}

func (m model) workshopContext(height int) string {
	title := gradient("Your loop")
	repo := strings.TrimPrefix(m.repo.Name, m.config.Org+"/")
	stage := "Shape the brief"
	if len(m.messages) > 0 {
		stage = "Refine the brief"
	}
	if m.prompt != "" {
		stage = "Draft ready"
	}
	section := func(label, value string) string {
		return lipgloss.NewStyle().Foreground(cyan).Bold(true).Render(label) + "\n" + lipgloss.NewStyle().Width(23).Render(safeText(value))
	}
	provider, codingModel, qualified := strings.Cut(m.options.Model, "/")
	if !qualified {
		provider, codingModel = "Runner default", m.options.Model
	}
	content := title + "\n\n" + section("Repository", repo) + "\n\n" + section("Prompt partner", m.config.AssistModel) + "\n\n" + section("Coding model", codingModel) + "\n\n" + section("Provider", provider) + "\n\n" + section("Budget", fmt.Sprintf("%d iterations · %dm each", m.options.Max, m.options.Timeout)) + "\n\n" + chip(stage, mint) + "\n" + dim.Render(fmt.Sprintf("%d messages", len(m.messages)))
	if height >= lipgloss.Height(content)+7 {
		content += "\n\n" + dim.Render("Ctrl+D  Draft a prompt\nCtrl+P  Review your text\nCtrl+K  All commands")
	}
	return panel.BorderForeground(violet).Width(28).Height(height).MaxHeight(height).Render(content)
}

// Reserve one column so the scroll marker never obscures message or log text.
func scrolledView(v viewport.Model) string {
	lines := strings.Split(v.View(), "\n")
	total, visible := v.TotalLineCount(), v.Height()
	thumb := max(1, visible*visible/max(1, total))
	top := int(float64(max(0, visible-thumb)) * v.ScrollPercent())
	for y := range lines {
		mark := " "
		if total > visible {
			mark = dim.Render("│")
			if y >= top && y < top+thumb {
				mark = lipgloss.NewStyle().Foreground(pink).Render("┃")
			}
		}
		lines[y] = lipgloss.NewStyle().Width(v.Width()).Render(lines[y]) + mark
	}
	return strings.Join(lines, "\n")
}

func scrollToPointer(v *viewport.Model, y, height int) {
	fraction := float64(max(0, min(height-1, y))) / float64(max(1, height-1))
	v.SetYOffset(int(fraction * float64(max(0, v.TotalLineCount()-v.Height()))))
}
