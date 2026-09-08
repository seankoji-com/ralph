package main

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func runBrief(r Run, fallback string) string {
	for _, line := range strings.Split(safeText(r.Prompt), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return fallback
}

func runOutcome(r Run) string {
	if r.External {
		return "External script log. Managed by its original script."
	}
	switch r.Status {
	case "complete":
		return "Task reported complete. Review the changes before publishing."
	case "budget reached":
		return "Iteration limit reached. Review the ledger for remaining work."
	case "failed", "interrupted", "orphaned", "unreadable":
		if r.Error != "" {
			return strings.Join(strings.Fields(safeText(r.Error)), " ")
		}
		return "Needs attention. Open Details for recovery information."
	case "stopped", "aborted":
		return "Stopped. Your worktree and logs are available in Details."
	case "cooldown":
		return "Resting before the next iteration."
	case "stopping":
		return "Finishing this iteration, then stopping."
	case "aborting":
		return "Stopping the current agent."
	case "queued", "preparing":
		return "Preparing a dedicated worktree."
	default:
		return "Working in its own worktree. You can leave Ralph open or quit."
	}
}

func (m model) deleteLayout(base stationLayout) stationLayout {
	r := *m.pendingDelete
	w := min(72, m.width-8)
	textWidth := w - 4
	body := lipgloss.NewStyle().Foreground(red).Bold(true).Render("Remove this loop?") + "\n\n" +
		lipgloss.NewStyle().Foreground(ink).Bold(true).Render(clip(r.Repo.Name, textWidth)) + "\n" +
		clip(r.ID, textWidth) + "\n" + clip(runBrief(r, "No saved brief"), textWidth) + "\n\n" +
		"Removes the worktree, branch and saved logs.\nThis cannot be undone.\n\n" +
		"Ralph preserves dirty, unmerged or active work."
	body = lipgloss.NewStyle().Width(textWidth).Render(body)
	keep, remove := chip("Esc  Keep loop", mint), lipgloss.NewStyle().Foreground(red).Padding(0, 1).Render("Y  Remove loop")
	popup := panel.BorderForeground(red).Background(mainBackground).Width(w).Render(body + "\n\n" + keep + " " + remove)
	x, y := (m.width-w)/2, (m.height-lipgloss.Height(popup))/2
	l := stationLayout{content: m.overlaySurface(base.content, popup, x, y)}
	buttonY := y + 1 + lipgloss.Height(body) + 1
	l.add("esc", x+2, buttonY, lipgloss.Width(keep), 1, 0)
	l.add("y", x+3+lipgloss.Width(keep), buttonY, lipgloss.Width(remove), 1, 0)
	return l
}
