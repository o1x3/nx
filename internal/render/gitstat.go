package render

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/o1x3/nx/internal/gitstat"
)

func GitStats(stats []gitstat.Stat) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7DD3FC")).
		Render("Git branch stats")

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E5E7EB")).
		Background(lipgloss.Color("#374151")).
		Padding(0, 1)

	cell := lipgloss.NewStyle().Padding(0, 1)
	muted := cell.Foreground(lipgloss.Color("#9CA3AF"))
	added := cell.Foreground(lipgloss.Color("#22C55E")).Bold(true)
	removed := cell.Foreground(lipgloss.Color("#EF4444")).Bold(true)
	warn := cell.Foreground(lipgloss.Color("#F59E0B"))

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers("repo", "branch", "base", "files", "added", "removed", "status").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return header
			}
			switch col {
			case 4:
				return added
			case 5:
				return removed
			case 6:
				return warn
			default:
				return cell
			}
		})

	for _, stat := range stats {
		status := "ok"
		if !stat.Fetched {
			status = stat.FetchNote
		}
		t.Row(
			stat.Name,
			stat.Head,
			stat.Base,
			fmt.Sprintf("%d", stat.Files),
			fmt.Sprintf("+%d", stat.Added),
			fmt.Sprintf("-%d", stat.Removed),
			status,
		)
	}

	totalAdded, totalRemoved, totalFiles := totals(stats)
	summary := muted.Render(fmt.Sprintf("%d repos  %d files  ", len(stats), totalFiles)) +
		added.Render(fmt.Sprintf("+%d", totalAdded)) +
		removed.Render(fmt.Sprintf(" -%d", totalRemoved))

	return title + "\n\n" + t.String() + "\n\n" + summary + "\n"
}

func totals(stats []gitstat.Stat) (int, int, int) {
	var added, removed, files int
	for _, stat := range stats {
		added += stat.Added
		removed += stat.Removed
		files += stat.Files
	}
	return added, removed, files
}
