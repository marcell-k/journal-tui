package tui

import (
	"database/sql"
	"fmt"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
	"github.com/marcell-k/journal-tui/internal/stats"
	"github.com/marcell-k/journal-tui/internal/util"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type tuiCorrelation struct {
	pairedDays              int
	rHours, rQuality, rFeel float64
	hours, quality, feel    []float64
	focus                   []float64
}

func loadTUICorrelation() (tuiCorrelation, error) {
	rows, err := tuiDB.Query(`
		SELECT c.sleep_hours, c.sleep_quality, c.feel, AVG(b.focus_quality)
		FROM daily_checkin c
		JOIN blocks b ON b.date = c.date AND b.focus_quality IS NOT NULL
		GROUP BY c.date
		ORDER BY c.date`)
	if err != nil {
		return tuiCorrelation{}, err
	}
	defer rows.Close()

	var hours, quality, feel, focus []float64
	for rows.Next() {
		var h, avgFocus float64
		var q, f float64
		if err := rows.Scan(&h, &q, &f, &avgFocus); err != nil {
			return tuiCorrelation{}, err
		}
		hours = append(hours, h)
		quality = append(quality, q)
		feel = append(feel, f)
		focus = append(focus, avgFocus)
	}
	if err := rows.Err(); err != nil {
		return tuiCorrelation{}, err
	}

	c := tuiCorrelation{
		pairedDays: len(hours),
		hours:      hours, quality: quality, feel: feel, focus: focus,
	}
	if len(hours) >= 3 {
		c.rHours = stats.Pearson(hours, focus)
		c.rQuality = stats.Pearson(quality, focus)
		c.rFeel = stats.Pearson(feel, focus)
	}
	return c, nil
}

func rValueStr(r float64) string {
	return fmt.Sprintf("r=%5.2f", r)
}

func rStyle(r float64) lipgloss.Style {
	abs := math.Abs(r)
	switch {
	case abs > 0.6:
		return focusHighStyle.Padding(0, 1)
	case abs >= 0.3:
		return focusMidStyle.Padding(0, 1)
	default:
		return dimStyle.Padding(0, 1)
	}
}
func rLine(r float64) string {
	abs := math.Abs(r)
	style := dimStyle
	switch {
	case abs > 0.6:
		style = focusHighStyle
	case abs >= 0.3:
		style = focusMidStyle
	}
	return style.Render(fmt.Sprintf("r=%5.2f  %-10s", r, scaledBar(abs, 1.0, 10)))
}

type tuiWeeklyTrend struct {
	sleepHours []float64
	sleepHas   []bool
	focusAvg   []float64
	focusHas   []bool
}

func loadWeeklyTrend(rangeStart string) (tuiWeeklyTrend, error) {
	t := tuiWeeklyTrend{
		sleepHours: make([]float64, trendDaysToShow),
		sleepHas:   make([]bool, trendDaysToShow),
		focusAvg:   make([]float64, trendDaysToShow),
		focusHas:   make([]bool, trendDaysToShow),
	}
	dates := rangeDates(rangeStart, trendDaysToShow)
	idx := make(map[string]int, 7)
	for i, d := range dates {
		idx[d] = i
	}

	sleepRows, err := tuiDB.Query(
		`SELECT date, sleep_hours, sleep_quality, feel, water_intake FROM daily_checkin WHERE date >= ? AND date <= ? ORDER BY date`,
		dates[0], dates[len(dates)-1],
	)
	if err != nil {
		return t, err
	}
	for sleepRows.Next() {
		var rawDate string
		var hours float64
		var quality, feel float64
		var water sql.NullFloat64
		if err := sleepRows.Scan(&rawDate, &hours, &quality, &feel, &water); err != nil {
			sleepRows.Close()
			return t, err
		}
		d := normalizeDate(rawDate)
		if i, ok := idx[d]; ok {
			t.sleepHours[i] = hours
			t.sleepHas[i] = true
		}
	}
	if err := sleepRows.Err(); err != nil {
		sleepRows.Close()
		return t, err
	}
	sleepRows.Close()

	focusRows, err := tuiDB.Query(
		`SELECT date, AVG(focus_quality) FROM blocks
		 WHERE date >= ? AND date <= ? AND focus_quality IS NOT NULL
		 GROUP BY date`,
		dates[0], dates[6],
	)
	if err != nil {
		return t, err
	}
	defer focusRows.Close()
	for focusRows.Next() {
		var date string
		var avg float64
		if err := focusRows.Scan(&date, &avg); err != nil {
			return t, err
		}
		if i, ok := idx[normalizeDate(date)]; ok {
			t.focusAvg[i] = avg
			t.focusHas[i] = true
		}
	}
	return t, focusRows.Err()
}
func normalizeDate(raw string) string {
	if pt, err := time.Parse(time.RFC3339, raw); err == nil {
		return pt.Format("2006-01-02")
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}

var sparkChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// sparkline renders one glyph per day; days with no data show '·'.
func toPlotSeries(values []float64, has []bool) []float64 {
	out := make([]float64, len(values))
	for i, v := range values {
		if has[i] {
			out[i] = v
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

var (
	subHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(colBlue)
	trendStyle     = lipgloss.NewStyle().Foreground(colBlue)

	mutedGoodStyle = lipgloss.NewStyle().Foreground(colSoftGreen)
	mutedMidStyle  = lipgloss.NewStyle().Foreground(colSoftGold)
	mutedBadStyle  = lipgloss.NewStyle().Foreground(colSoftRed)
)

func mutedFocus(n float64) lipgloss.Style {
	switch {
	case n <= 0:
		return dimStyle
	case n <= 3:
		return mutedBadStyle
	case n <= 7:
		return mutedMidStyle
	default:
		return mutedGoodStyle
	}
}

// barTrack draws a filled bar plus a dim track so the scale is visible.
func barTrack(value, top float64, width int, st lipgloss.Style) string {
	w := 0
	if top > 0 {
		w = int(math.Round(value / top * float64(width)))
	}
	w = min(max(w, 0), width)
	return st.Render(strings.Repeat("■", w)) + tableBorderStyle.Render(strings.Repeat("·", width-w))
}

// weekDayAxis prints one letter per day under a plot, today highlighted.
func weekDayAxis(graph string, numDays, perDay int) string {
	firstLine, _, _ := strings.Cut(graph, "\n")
	margin := 0
	// The axis glyph is ┤ normally, but ┼ when the line touches the axis.
	if i := strings.IndexAny(firstLine, "┤┼"); i >= 0 {
		margin = i + 1
	}
	letters := []string{"M", "T", "W", "T", "F", "S", "S"}
	today := numDays - 7 + todayDayIndex()
	sep := strings.Repeat(" ", perDay-1)

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", margin))
	for i := 0; i < numDays; i++ {
		if i == today {
			b.WriteString(cursorStyle.Bold(true).Render(letters[i%7]))
		} else {
			b.WriteString(dimStyle.Render(letters[i%7]))
		}
		if i < numDays-1 {
			b.WriteString(sep)
		}
	}
	return b.String()
}

func trendPlot(title string, values []float64, has []bool, style lipgloss.Style, perDay int) string {
	var sum float64
	n := 0
	for i, v := range values {
		if has[i] {
			sum += v
			n++
		}
	}
	head := style.Bold(true).Render(title)
	if n == 0 {
		return head + "\n" + dimStyle.Render("no data yet")
	}
	head += dimStyle.Render(fmt.Sprintf("  avg %.1f", sum/float64(n)))

	graph := asciigraph.Plot(
		toPlotSeries(values, has),
		asciigraph.Height(5),
		asciigraph.Width(len(values)*perDay),
		asciigraph.Precision(1),
	)
	return head + "\n" + style.Render(graph) + "\n" + weekDayAxis(graph, len(values), perDay)
}

func (m tuiModel) blockPosSection() string {
	const slots = 5
	byNum := make(map[int]tuiBlockPosStat, len(m.blockPosStats))
	for _, s := range m.blockPosStats {
		byNum[s.num] = s
	}
	var b strings.Builder
	b.WriteString(subHeaderStyle.Render("By block #"))
	b.WriteString("\n")
	for n := 1; n <= slots; n++ {
		s, ok := byNum[n]
		avg, avgStr, st := 0.0, "-", dimStyle
		if ok {
			avg = s.avg
			avgStr = fmt.Sprintf("%.1f", s.avg)
			st = mutedFocus(s.avg)
		}
		fmt.Fprintf(&b, "#%-3d %s %4s  %s\n", n,
			barTrack(avg, 10, 10, st), avgStr, dimStyle.Render(fmt.Sprintf("n=%d", s.count)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m tuiModel) startTimeSection() string {
	var b strings.Builder
	b.WriteString(subHeaderStyle.Render("By start time"))
	b.WriteString("\n")
	for _, s := range m.startTimeStats {
		avg, avgStr, st := 0.0, "-", dimStyle
		if s.count > 0 {
			avg = s.sum / float64(s.count)
			avgStr = fmt.Sprintf("%.1f", avg)
			st = mutedFocus(avg)
		}
		fmt.Fprintf(&b, "%s %s %4s  %s\n", s.label,
			barTrack(avg, 10, 10, st), avgStr, dimStyle.Render(fmt.Sprintf("n=%d", s.count)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m tuiModel) distSection() string {
	var b strings.Builder
	b.WriteString(subHeaderStyle.Render("Distribution"))
	b.WriteString("\n")
	labels := [5]string{"1-2 ", "3-4 ", "5-6 ", "7-8 ", "9-10"}
	mids := [5]float64{1.5, 3.5, 5.5, 7.5, 9.5}
	top := 0
	for _, c := range m.focusHist {
		top = max(top, c)
	}
	for i, c := range m.focusHist {
		fmt.Fprintf(&b, "%s %s %3d\n", labels[i],
			barTrack(float64(c), float64(top), 10, mutedFocus(mids[i])), c)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m tuiModel) viewMetrics() string {
	var b strings.Builder

	// ----- weekly trend -----
	b.WriteString(headerStyle.Render("=== Weekly trend ==="))
	b.WriteString("\n")
	// Each plot is ~8 label columns + 2 columns per day. Side by side if it fits.
	sideBySide := m.width >= 2*(trendDaysToShow*2+8)+4
	perDay := 3
	if sideBySide {
		perDay = 2
	}
	sleepPlot := trendPlot("Sleep (hrs)", m.weeklyTrend.sleepHours, m.weeklyTrend.sleepHas, trendStyle, perDay)
	focusPlot := trendPlot("Focus (avg)", m.weeklyTrend.focusAvg, m.weeklyTrend.focusHas, trendStyle, perDay)
	if sideBySide {
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, sleepPlot, "    ", focusPlot))
	} else {
		b.WriteString(sleepPlot)
		b.WriteString("\n\n")
		b.WriteString(focusPlot)
	}
	b.WriteString("\n\n")

	// ----- blocks by project -----
	b.WriteString(headerStyle.Render("=== Blocks by project (this week) ==="))
	b.WriteString("\n")
	if len(m.metrics) == 0 {
		b.WriteString(dimStyle.Render("No blocks logged this week."))
		b.WriteString("\n")
	} else {
		top := 0
		for _, mt := range m.metrics {
			top = max(top, mt.count)
		}
		for _, mt := range m.metrics {
			ps := projectStyle(mt.project)
			fmt.Fprintf(&b, "  %s %s %s  avg focus %s\n",
				ps.Render(fmt.Sprintf("%-15s", mt.project)),
				barTrack(float64(mt.count), float64(top), 10, ps),
				dimStyle.Render(fmt.Sprintf("%2d", mt.count)),
				mutedFocus(mt.avgFocus).Render(fmt.Sprintf("%.1f", mt.avgFocus)))
		}
	}

	// ----- correlation -----
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("=== Correlation (all-time, paired days) ==="))
	b.WriteString("\n")
	if m.correlation.pairedDays < 3 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("Only %d paired days found. Need at least 3 (ideally 14+).\n", m.correlation.pairedDays)))
	} else {
		c := m.correlation
		gap := "    "
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			scatterPlot("sleep_hours", c.hours, c.focus, c.rHours), gap,
			scatterPlot("sleep_quality", c.quality, c.focus, c.rQuality), gap,
			scatterPlot("feel", c.feel, c.focus, c.rFeel),
		))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("y: avg focus per day  •  %d paired days", c.pairedDays)))
		b.WriteString("\n")
	}

	// ----- focus stats (three columns) -----
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("=== Focus (all-time) ==="))
	b.WriteString("\n")
	if m.focusN == 0 {
		b.WriteString(dimStyle.Render("No closed blocks yet.\n"))
		return b.String()
	}
	col := "      "
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
		m.blockPosSection(), col, m.startTimeSection(), col, m.distSection()))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("median %.0f  •  mean %.1f  •  %d blocks", m.focusMedian, m.focusMean, m.focusN)))
	b.WriteString("    ")
	b.WriteString(mutedGoodStyle.Render(fmt.Sprintf("best %s %.1f", m.weekdayBest.day, m.weekdayBest.avg)))
	b.WriteString("  ")
	b.WriteString(mutedBadStyle.Render(fmt.Sprintf("worst %s %.1f", m.weekdayWorst.day, m.weekdayWorst.avg)))
	b.WriteString("\n")
	return b.String()
}

// weekGoalStats returns done/total goal counts for a week.
func weekGoalStats(w goalsWeek) (done, total int) {
	for _, d := range w.days {
		for _, g := range d.goals {
			total++
			if g.done {
				done++
			}
		}
	}
	return
}

const completionBarWidth = 5

func completionBar(done, total int) string {
	filled := int(math.Round(float64(done) / float64(total) * float64(completionBarWidth)))
	filled = min(filled, completionBarWidth)
	bar := strings.Repeat("■", filled) + strings.Repeat(".", completionBarWidth-filled)

	return fmt.Sprintf("%s %d/%d done", bar, done, total)
}

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "OPEN":
		return okStyle
	case "CLOSED", "DONE":
		return dimStyle
	default:
		return tableCellStyle
	}
}
func loadGoalWeeks(currentWeekStart string, count int) ([]goalsWeek, error) {
	firstStart, err := firstWeekStart()

	if err != nil {
		return nil, err
	}
	var out []goalsWeek
	t, _ := time.Parse("2006-01-02", currentWeekStart)
	for i := range count {
		ws := t.AddDate(0, 0, -7*i).Format("2006-01-02")
		w, err := loadGoalWeek(ws, weekNumFor(ws, firstStart))
		if err != nil {
			return nil, err
		}
		out = append(out, w)
		if firstStart != "" && ws <= firstStart {
			break
		}
	}
	return out, nil
}

// viewForm renders whatever form is currently active (new/update block,
// close block, add/edit goal, add/update sleep checkin, add/rename project).

type tuiBlockPosStat struct {
	num   int
	avg   float64
	count int
}

func loadBlockPosStats() ([]tuiBlockPosStat, error) {
	rows, err := tuiDB.Query(`
		SELECT block_num, AVG(focus_quality), COUNT(*)
		FROM blocks WHERE focus_quality IS NOT NULL
		GROUP BY block_num ORDER BY block_num`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []tuiBlockPosStat
	for rows.Next() {
		var s tuiBlockPosStat
		if err := rows.Scan(&s.num, &s.avg, &s.count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type tuiWeekdayStat struct {
	day string
	avg float64
}

func loadWeekdayExtremes() (best, worst tuiWeekdayStat, err error) {
	rows, err := tuiDB.Query(`
		SELECT day, AVG(focus_quality)
		FROM blocks WHERE focus_quality IS NOT NULL
		GROUP BY day`)
	if err != nil {
		return best, worst, err
	}
	defer rows.Close()

	first := true
	for rows.Next() {
		var s tuiWeekdayStat
		if err := rows.Scan(&s.day, &s.avg); err != nil {
			return best, worst, err
		}
		if first || s.avg > best.avg {
			best = s
		}
		if first || s.avg < worst.avg {
			worst = s
		}
		first = false
	}
	return best, worst, rows.Err()
}

// loadFocusDistribution buckets all logged focus_quality values into
// [1-2,3-4,5-6,7-8,9-10] and returns median/mean alongside.
func loadFocusDistribution() (hist [5]int, median, mean float64, n int, err error) {
	rows, err := tuiDB.Query(`SELECT focus_quality FROM blocks WHERE focus_quality IS NOT NULL ORDER BY focus_quality`)
	if err != nil {
		return hist, 0, 0, 0, err
	}
	defer rows.Close()

	var vals []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return hist, 0, 0, 0, err
		}
		vals = append(vals, v)
	}
	if err := rows.Err(); err != nil {
		return hist, 0, 0, 0, err
	}

	n = len(vals)
	if n == 0 {
		return hist, 0, 0, 0, nil
	}

	sum := 0.0
	for _, v := range vals {
		bucket := int(math.Floor((v - 1) / 2))
		bucket = min(max(bucket, 0), 4)
		hist[bucket]++ // 1-2->0, 3-4->1, 5-6->2, 7-8->3, 9-10->4
		sum += v
	}
	mean = sum / float64(n)

	sort.Float64s(vals)
	if n%2 == 1 {
		median = vals[n/2]
	} else {
		median = (vals[n/2-1] + vals[n/2]) / 2
	}
	return hist, median, mean, n, nil
}

func (m tuiModel) submitBlockAppendNote() (tea.Model, tea.Cmd) {
	note := strings.TrimSpace(m.f.values[0])
	if note == "" {
		m.f.errMsg = "Note can't be blank."
		return m, nil
	}
	_, err := tuiDB.Exec(
		`UPDATE blocks SET done_notes = CASE
			WHEN done_notes IS NULL OR done_notes = '' THEN ?
			ELSE done_notes || ' | ' || ?
		 END WHERE id = ?`,
		note, note, m.f.ctxID,
	)
	if err != nil {
		m.err = err
		m.mode = "browse"
		return m, nil
	}
	m.mode = "browse"
	m.status = "Note appended."
	if rerr := m.reload(); rerr != nil {
		m.err = rerr
	} else {
		m.err = nil
	}
	return m, nil
}

// scaledBar renders a bar of up to maxWidth chars for value out of max.
func scaledBar(value, max float64, maxWidth int) string {
	if max <= 0 {
		return ""
	}
	w := int(math.Round(value / max * float64(maxWidth)))
	w = min(w, maxWidth)
	if w < 0 {
		w = 0
	}
	return strings.Repeat("■", w)
}

type tuiStartTimeStat struct {
	label string
	sum   float64
	count int
}

var startTimeBuckets = []struct {
	label      string
	start, end int
}{
	{"4-8  ", 4, 8},
	{"8-12 ", 8, 12},
	{"12-16", 12, 16},
	{"16-20", 16, 20},
	{"20-24", 20, 24},
}

func loadStartTimeStats() ([]tuiStartTimeStat, error) {
	rows, err := tuiDB.Query(`SELECT created_at, focus_quality FROM blocks WHERE focus_quality IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sums := make([]float64, len(startTimeBuckets))
	counts := make([]int, len(startTimeBuckets))

	for rows.Next() {
		var createdAt string
		var focus float64
		if err := rows.Scan(&createdAt, &focus); err != nil {
			return nil, err
		}
		t, perr := time.Parse("2006-01-02 15:04:05", createdAt)
		if perr != nil {
			if t2, perr2 := time.Parse(time.RFC3339, createdAt); perr2 == nil {
				t = t2
			} else {
				continue
			}
		}
		hour := t.Hour()
		for i, bucket := range startTimeBuckets {
			if hour >= bucket.start && hour < bucket.end {
				sums[i] += focus
				counts[i]++
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]tuiStartTimeStat, len(startTimeBuckets))
	for i, bucket := range startTimeBuckets {
		out[i] = tuiStartTimeStat{label: bucket.label, sum: sums[i], count: counts[i]}
	}
	return out, nil
}

func (m tuiModel) viewBlockDetail() string {
	d := m.blockDetail

	var b strings.Builder
	header := fmt.Sprintf("Block #%d (id=%d) — %s (%s)", d.blockNum, d.id, dateOnly(d.date), d.day)
	if d.closedAt != nil {
		closedTime := *d.closedAt
		if ct, err := util.ParseTimestamp(*d.closedAt); err == nil {
			closedTime = ct.Format("15:04")
		}
		header += fmt.Sprintf(" — closed %s", closedTime)
	}
	b.WriteString(bannerStyle.Render(header))
	b.WriteString("\n")

	row := func(label, val string) {
		fmt.Fprintf(&b, "%-16s %s\n", label+":", val)
	}

	row("Project", d.project)
	row("Outcome", d.outcome)
	row("Context reload", d.contextReload)
	row("Deliverable", d.deliverable)
	row("Done", d.doneNotes)
	row("Not done", d.notDoneNotes)
	row("Next step", d.nextStep)
	row("Files/links", d.filesLinks)
	focus := "-"
	if d.focus != nil {
		focus = strconv.FormatFloat(*d.focus, 'f', 1, 64)
	}
	row("Focus quality", focus)
	row("Tweak", d.tweak)
	// One dim summary line: created → closed · duration
	created := d.createdAt
	var createdT time.Time
	createdOK := false
	if ct, err := util.ParseTimestamp(d.createdAt); err == nil {
		created = ct.Format("2006-01-02 15:04")
		createdT, createdOK = ct, true
	}
	line := created + " → open"
	if d.closedAt != nil {
		closed, dur := *d.closedAt, ""
		if ct2, err := util.ParseTimestamp(*d.closedAt); err == nil {
			closed = ct2.Format("15:04")
			if createdOK {
				dur = " · " + util.FormatDuration(ct2.Sub(createdT))
			}
		}
		line = created + " → " + closed + dur
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(line))
	b.WriteString("\n")

	return b.String()
}

// ---- plot correlation using scatter ----
const (
	scatterW = 20
	scatterH = 8
)

func rColor(r float64) lipgloss.Color {
	switch abs := math.Abs(r); {
	case abs > 0.6:
		return colGreen
	case abs >= 0.3:
		return colGold
	default:
		return colMuted
	}
}

func dataBounds(v []float64) (lo, hi float64) {
	lo, hi = v[0], v[0]
	for _, x := range v {
		lo = min(lo, x)
		hi = max(hi, x)
	}
	if hi-lo < 1e-9 {
		lo -= 0.5
		hi += 0.5
	}
	return
}

// scatterPlot draws xs (horizontal) against ys (vertical) on a character grid.
// Cells with one point show •, cells with several show ●.
func scatterPlot(name string, xs, ys []float64, r float64) string {
	minX, maxX := dataBounds(xs)
	minY, maxY := dataBounds(ys)

	grid := make([][]int, scatterH)
	for i := range grid {
		grid[i] = make([]int, scatterW)
	}
	for i := range xs {
		col := int(math.Round((xs[i] - minX) / (maxX - minX) * float64(scatterW-1)))
		row := scatterH - 1 - int(math.Round((ys[i]-minY)/(maxY-minY)*float64(scatterH-1)))
		grid[row][col]++
	}

	pt := lipgloss.NewStyle().Foreground(rColor(r))
	var b strings.Builder
	const scatterIndent = 7
	title := fmt.Sprintf("%s  r=%5.2f", name, r)
	pad := max((scatterW-len(title))/2, 0)
	b.WriteString(strings.Repeat(" ", scatterIndent+pad))
	b.WriteString(pt.Bold(true).Render(title))
	b.WriteString("\n")
	for row := 0; row < scatterH; row++ {
		label := "     "
		switch row {
		case 0:
			label = fmt.Sprintf("%5.1f", maxY)
		case scatterH - 1:
			label = fmt.Sprintf("%5.1f", minY)
		}
		b.WriteString(dimStyle.Render(label + " │"))
		for col := 0; col < scatterW; col++ {
			switch n := grid[row][col]; {
			case n == 1:
				b.WriteString(pt.Render("•"))
			case n > 1:
				b.WriteString(pt.Render("●"))
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("      └" + strings.Repeat("─", scatterW)))
	b.WriteString("\n")
	lo, hi := fmt.Sprintf("%.1f", minX), fmt.Sprintf("%.1f", maxX)
	gap := max(scatterW-len(lo)-len(hi), 1)
	b.WriteString(dimStyle.Render("       " + lo + strings.Repeat(" ", gap) + hi))
	return b.String()
}
