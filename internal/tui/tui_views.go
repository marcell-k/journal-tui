package tui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/marcell-k/journal-tui/internal/util"
)

// ---------- View ----------

func (m tuiModel) View() string {
	var b strings.Builder
	b.WriteString(m.headerBar())
	b.WriteString("\n")

	tabs := make([]string, tabCount)
	for i, name := range tabNames {
		if i == m.tab {
			tabs[i] = activeTabStyle.Render(name)
		} else {
			tabs[i] = inactiveTabStyle.Render(name)
		}
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tabs...))
	b.WriteString("\n\n")

	switch m.mode {
	case "form":
		b.WriteString(m.viewForm())
	case "start_project":
		b.WriteString(m.viewStartProject())
	case "block_detail":
		b.WriteString(m.viewBlockDetail())
	case "confirm_delete":
		b.WriteString(m.viewConfirmDelete())
	default:
		switch m.tab {
		case tabBlocks:
			b.WriteString(m.viewBlocks())
		case tabGoals:
			b.WriteString(m.viewGoals())
		case tabSleep:
			b.WriteString(m.viewSleep())
		case tabProjects:
			b.WriteString(m.viewProjects())
		case tabMetrics:
			b.WriteString(m.viewMetrics())
		}
	}

	body := b.String()

	var f strings.Builder
	if m.err != nil {
		f.WriteString(errStyle.Render("Error: " + m.err.Error()))
		f.WriteString("\n")
	}
	if m.status != "" {
		f.WriteString(okStyle.Render(m.status))
		f.WriteString("\n")
	}
	f.WriteString(dimStyle.Render(m.helpLine()))
	footer := f.String()

	// Pad so the footer lands on the last row
	pad := m.height - lipgloss.Height(body) - lipgloss.Height(footer) + 1
	if pad < 1 {
		pad = 1 // content taller than the window: keep one separator line
	}
	return body + strings.Repeat("\n", pad) + footer
}

func (m tuiModel) headerBar() string {
	now := time.Now().In(displayLoc)

	if m.openBlock == nil {
		return dimStyle.Render("○ No open block")
	}

	ob := m.openBlock
	elapsed := now.Sub(ob.startedAt)
	elapsed = max(elapsed, 0)
	endsAt := ob.startedAt.Add(time.Duration(blockLenMins) * time.Minute)
	remaining := endsAt.Sub(now)

	elapsedStr := util.FormatDuration(elapsed)

	timerStyle := okStyle
	statusWord := fmt.Sprintf("ends %s", endsAt.Format("15:04"))
	if remaining < 0 {
		timerStyle = errStyle
		statusWord = fmt.Sprintf("over by %s", util.FormatDuration(remaining))
	}

	return timerStyle.Render(fmt.Sprintf("● Block #%d open (", ob.blockNum)) +
		projectStyle(ob.project).Bold(true).Render(ob.project) +
		timerStyle.Render(fmt.Sprintf(") — %s elapsed, %s — %s",
			elapsedStr, statusWord, ob.outcome))
}

func (m tuiModel) helpLine() string {
	switch m.mode {
	case "form":
		return "tab/↓: next field  •  shift+tab/↑: prev field  •  enter (last field): save  •  esc: cancel"
	case "confirm_delete":
		return "y/enter: confirm delete  •  n/esc: cancel"
	case "start_project":
		return "↑↓/jk: move  •  enter: pick project  •  esc: cancel"
	case "block_detail":
		return "esc/enter/q: back to list"
	}
	switch m.tab {
	case tabBlocks:
		return "ctrl+n/b: switch section  •  ↑↓/jk: move  •  enter: view block  •  n: new  •  s: log shallow  •  u: update  •  c: close  •  d: delete  •  r: reload  •  q: quit"
	case tabGoals:
		return "ctrl+n/b: switch section  •  ↑↓/jk: move  •  ctrl+j/k: reorder  •  n: new  •  u: update  •  d: delete  •  enter/space: toggle done  •  r: reload  •  q: quit"
	case tabSleep:
		return "ctrl+n/b: switch section  •  ↑↓/jk: move  •  n: new  •  u: update  •  d: delete  •  r: reload  •  q: quit"
	case tabProjects:
		return "ctrl+n/b: switch section  •  ↑↓/jk: move  •  enter: edit notes in nvim  •  n: new  •  u: rename  •  d: delete  •  r: reload  •  q: quit"
	default:
		return "ctrl+n/b: switch section  •  r: reload  •  q: quit"
	}
}

func focusStyle(n float64) lipgloss.Style {
	switch {
	case n <= 0:
		return tableCellStyle
	case n <= 3:
		return focusLowStyle
	case n <= 7:
		return focusMidStyle
	default:
		return focusHighStyle
	}
}

const clampColWidth = 15

var outcomeColWidth = int(float64(clampColWidth) * 1.2)

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func (m tuiModel) viewBlocks() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("=== Blocks (this week) ==="))
	b.WriteString("\n")

	if len(m.blocks) == 0 {
		b.WriteString(dimStyle.Render("No blocks logged this week."))
		return b.String()
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(tableBorderStyle).
		Headers("#", "Date", "Type", "Status", "Project", "Length", "Focus", "Outcome").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				if col == 5 || col == 6 { // Length, Focus
					return tableHeaderStyle.Align(lipgloss.Right)
				}
				return tableHeaderStyle
			}
			blk := m.blocks[row]
			selected := row == m.blockCur
			switch col {
			case 2: // Type
				if selected {
					return rowSelectedStyle.Align(lipgloss.Center)
				}
				return tableCellStyle.Align(lipgloss.Center)
			case 3: // Status
				if selected {
					return rowSelectedStyle.Align(lipgloss.Center)
				}
				status := "DONE"
				if !blk.closed {
					status = "OPEN"
				}
				return statusStyle(status).Padding(0, 1).Align(lipgloss.Center)
			case 4: // Project
				if selected {
					return rowSelectedStyle
				}
				return projectStyle(blk.project).Padding(0, 1)
			case 5: // Length
				if selected {
					return rowSelectedStyle.Align(lipgloss.Right)
				}
				return tableCellStyle.Align(lipgloss.Right)
			case 6: // Focus
				if selected {
					return rowSelectedStyle.Align(lipgloss.Right)
				}
				if blk.focus != nil {
					return focusStyle(*blk.focus).Align(lipgloss.Right)
				}
				return tableCellStyle.Align(lipgloss.Right)
			default:
				if selected {
					return rowSelectedStyle
				}
				return tableCellStyle
			}
		})

	for i, blk := range m.blocks {
		marker := " "
		if i == m.blockCur {
			marker = "▶"
		}
		idStr := fmt.Sprintf("%s%*d", marker, 3, blk.blockNum)
		status := "DONE"
		if !blk.closed {
			status = "OPEN"
		}
		focus := "-"
		if blk.focus != nil {
			focus = formatFocus(*blk.focus)
		}
		t.Row(idStr, dateOnly(blk.date), typeGlyph(blk.blockType), truncate(status, clampColWidth), truncate(blk.project, clampColWidth), truncate(blk.length, clampColWidth), truncate(focus, clampColWidth), truncate(blk.outcome, outcomeColWidth))
	}

	b.WriteString(t.Render())
	b.WriteString("\n")
	return b.String()
}

func formatFocus(f float64) string {
	if f == math.Trunc(f) {
		return strconv.Itoa(int(f))
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func (m tuiModel) viewGoals() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("=== Weekly Goals ==="))
	b.WriteString("\n")

	if len(m.goalWeeks) == 0 {
		b.WriteString(dimStyle.Render("No goals yet — press n to add one."))
		return b.String()
	}

	todayIdx := todayDayIndex()

	for wi, w := range m.goalWeeks {
		arrow := "▸"
		if wi == m.goalWeekExpanded {
			arrow = "▾"
		}
		marker := "  "
		if wi == m.goalCurWeek && m.goalCurDay == -1 {
			marker = cursorStyle.Render("> ")
		}
		done, total := weekGoalStats(w)
		fmt.Fprintf(&b, "%s%s Week %s – %s  #%d   %s\n",
			marker, arrow, w.weekStart, w.weekEnd, w.num, completionBar(done, total))

		if wi != m.goalWeekExpanded {
			continue
		}

		start := 0
		if wi == 0 {
			start = m.goalRevealFrom
		}
		for di := start; di < 7; di++ {
			day := w.days[di]
			dayMarker := "    "
			if wi == m.goalCurWeek && m.goalCurDay == di && m.goalCurGoal == -1 {
				dayMarker = cursorStyle.Render("  > ")
			}
			label := day.name
			if wi == 0 && di == todayIdx {
				label = okStyle.Render(day.name + " (today)")
			}
			b.WriteString(dayMarker)
			b.WriteString(label)
			b.WriteString("\n")

			if len(day.goals) == 0 {
				b.WriteString("        ")
				b.WriteString(dimStyle.Render("(no goals)"))
				b.WriteString("\n")
				continue
			}
			for gi, g := range day.goals {
				mark := " "
				if g.done {
					mark = "x"
				}
				goalMarker := "      "
				if wi == m.goalCurWeek && m.goalCurDay == di && m.goalCurGoal == gi {
					goalMarker = cursorStyle.Render("    > ")
				}
				fmt.Fprintf(&b, "%s[%s] %s\n", goalMarker, mark, g.goal)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m tuiModel) viewSleep() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("=== Wellness (this week) ==="))
	b.WriteString("\n")

	if len(m.sleep) == 0 {
		b.WriteString(dimStyle.Render("No checkins logged this week — press n to add one."))
		return b.String()
	}
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(tableBorderStyle).
		Headers("", "Date", "Sleep", "Quality", "Feel", "Water", "Day").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeaderStyle
			}
			s := m.sleep[row]
			selected := row == m.sleepCur
			switch col {
			case 3: // Quality
				if selected {
					return rowSelectedStyle
				}
				return focusStyle(s.quality)
			case 4: // Feel
				if selected {
					return rowSelectedStyle
				}
				return focusStyle(s.feel)
			default:
				if selected {
					return rowSelectedStyle
				}
				return tableCellStyle
			}
		})

	var sumHours, sumWater float64
	var sumQuality, sumFeel float64
	var waterN int
	for i, s := range m.sleep {
		marker := " "
		if i == m.sleepCur {
			marker = "▶"
		}
		waterStr := "-"
		if s.hasWater {
			waterStr = fmt.Sprintf("%.1fL", s.water)
			sumWater += s.water
			waterN++
		}
		t.Row(marker, s.date, fmt.Sprintf("%.1fh", s.hours), strconv.FormatFloat(s.quality, 'f', 1, 64), strconv.FormatFloat(s.feel, 'f', 1, 64), waterStr, s.weekday)

		sumHours += s.hours
		sumQuality += s.quality
		sumFeel += s.feel
	}
	b.WriteString(t.Render())
	b.WriteString("\n")
	n := float64(len(m.sleep))
	avgWaterStr := "-"
	if waterN > 0 {
		avgWaterStr = fmt.Sprintf("%.1fL", sumWater/float64(waterN))
	}
	fmt.Fprintf(&b, "\nAvg sleep: %.1fh | Avg quality: %.1f | Avg feel: %.1f | Avg water: %s\n",
		sumHours/n, sumQuality/n, sumFeel/n, avgWaterStr)

	return b.String()
}

func (m tuiModel) viewProjects() string {
	if len(m.projects) == 0 {
		return headerStyle.Render("=== Projects ===") + "\n" + dimStyle.Render("No projects yet — press n to add one.")
	}

	var b strings.Builder
	b.WriteString(headerStyle.Render("=== Projects ==="))
	b.WriteString("\n")

	for i, p := range m.projects {
		marker := "○"
		if hasNotes(p.id) {
			marker = "●"
		}
		line := fmt.Sprintf("%s %d) %s", marker, p.id, p.name)
		if i == m.projectCur {
			b.WriteString(cursorStyle.Render("> " + line))
		} else {
			b.WriteString("  ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	sel := m.projects[m.projectCur]
	b.WriteString("\n")
	b.WriteString(headerStyle.Render(fmt.Sprintf("=== Notes: %s ===", sel.name)))
	b.WriteString("\n")
	if notes, ok := readNotes(sel.id, notePreviewLines); ok {
		b.WriteString(notes)
	} else {
		b.WriteString(dimStyle.Render("No notes yet — press enter to write some."))
	}
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) viewForm() string {
	var b strings.Builder
	b.WriteString(bannerStyle.Render(m.f.title))
	b.WriteString("\n")

	labelWidth := 0
	for _, l := range m.f.labels {
		if len(l) > labelWidth {
			labelWidth = len(l)
		}
	}

	for i, label := range m.f.labels {
		marker := "  "
		if i == m.f.field {
			marker = cursorStyle.Render("> ")
		}
		val := strings.ReplaceAll(m.f.values[i], "\n", dimStyle.Render("")+"\n"+strings.Repeat(" ", labelWidth+3))
		fmt.Fprintf(&b, "%s%-*s %s\n", marker, labelWidth+1, label+":", val)
	}
	if m.f.errMsg != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.f.errMsg))
		b.WriteString("\n")
	}
	return b.String()
}

func (m tuiModel) viewStartProject() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("=== New block: pick a project ==="))
	b.WriteString("\n")

	maxNameLen := 0
	for _, p := range m.projects {
		if len(p.name) > maxNameLen {
			maxNameLen = len(p.name)
		}
	}

	for i, p := range m.projects {
		namePadded := fmt.Sprintf("%-*s", maxNameLen, p.name)
		var hint string
		if p.nextStep != "" {
			hint = " " + dimStyle.Render("("+p.nextStep+")")
		}
		if i == m.projectCur {
			b.WriteString(cursorStyle.Render("> " + namePadded))
			b.WriteString(hint)
		} else {
			b.WriteString("  ")
			b.WriteString(namePadded)
			b.WriteString(hint)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m tuiModel) viewConfirmDelete() string {
	var b strings.Builder
	b.WriteString(bannerStyle.Render("Confirm delete"))
	b.WriteString("\n")
	b.WriteString(errStyle.Render(fmt.Sprintf("Delete %s? This cannot be undone.", m.pendingDelete.label)))
	b.WriteString("\n")
	return b.String()
}

func readNotes(projectID, maxLines int) (string, bool) {
	path := filepath.Join(notesDir, fmt.Sprintf("%d.md", projectID))
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], "…")
	}
	return strings.Join(lines, "\n"), true
}
