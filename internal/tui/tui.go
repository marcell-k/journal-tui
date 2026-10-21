package tui

import (
	"database/sql"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------- tabs ----------

const (
	tabBlocks = iota
	tabGoals
	tabSleep
	tabProjects
	tabMetrics
	tabCount
)

const weeksToShow = 3
const trendDaysToShow = weeksToShow * 7

const notesDir = "notes"
const notePreviewLines = 15

var tabNames = [tabCount]string{"Blocks", "Goals", "Wellness", "Projects", "Metrics"}

type notesEditorMsg struct{ err error }

func openNoteEditor(projectID int) tea.Cmd {
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return func() tea.Msg { return notesEditorMsg{err: err} }
	}
	path := filepath.Join(notesDir, fmt.Sprintf("%d.md", projectID))
	c := exec.Command("nvim", path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return notesEditorMsg{err: err}
	})
}

func hasNotes(projectID int) bool {
	path := filepath.Join(notesDir, fmt.Sprintf("%d.md", projectID))
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// ---------- styles ----------
const (
	// accents
	colGold = lipgloss.Color("#e6c384") // primary: headers, cursor, selection, active tab
	colBlue = lipgloss.Color("#8ba4b0") // secondary: banners, sub-headers, trends, deep blocks

	// text and lines
	colText   = lipgloss.Color("#c9c9c1")
	colMuted  = lipgloss.Color("#a6a69c") // dim text, inactive tabs
	colFaint  = lipgloss.Color("#6b6b64")
	colBorder = lipgloss.Color("#4a4a45")

	// status (bright)
	colRed   = lipgloss.Color("#e46876") // error, overrun
	colGreen = lipgloss.Color("#87a987") // ok, open

	// soft variants for charts and ratings
	colSoftRed   = lipgloss.Color("#c4746e")
	colSoftGreen = lipgloss.Color("#8a9a7b")
	colSoftGold  = lipgloss.Color("#c4b28a")

	// project-only colors
	colMauve = lipgloss.Color("#a292a3")
	colSky   = lipgloss.Color("#7fb4ca")
)

var (
	activeTabStyle   = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(colGold).Underline(true)
	inactiveTabStyle = lipgloss.NewStyle().Padding(0, 2).Foreground(colMuted)
	headerStyle      = lipgloss.NewStyle().Bold(true).Foreground(colGold)
	tableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(colGold).Padding(0, 1)
	tableCellStyle   = lipgloss.NewStyle().Padding(0, 1)
	tableBorderStyle = lipgloss.NewStyle().Foreground(colBorder)
	rowSelectedStyle = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(colGold)
	focusHighStyle   = lipgloss.NewStyle().Padding(0, 1).Foreground(colGreen)
	focusMidStyle    = lipgloss.NewStyle().Padding(0, 1).Foreground(colGold)
	focusLowStyle    = lipgloss.NewStyle().Padding(0, 1).Foreground(colRed)
	nextStepStyle    = lipgloss.NewStyle().Italic(true).Foreground(colFaint)

	deepTypeStyle    = lipgloss.NewStyle().Foreground(colBlue)
	shallowTypeStyle = lipgloss.NewStyle().Foreground(colGold)

	bannerStyle = lipgloss.NewStyle().Bold(true).Foreground(colBlue).
			Border(lipgloss.RoundedBorder()).Padding(0, 1).MarginTop(1).MarginBottom(1)
	cursorStyle = lipgloss.NewStyle().Foreground(colGold)
	dimStyle    = lipgloss.NewStyle().Foreground(colMuted)
	errStyle    = lipgloss.NewStyle().Foreground(colRed)
	okStyle     = lipgloss.NewStyle().Foreground(colGreen)
)
var projectPalette = []lipgloss.Color{
	colSoftRed,
	colSoftGreen,
	colSoftGold,
	colSky,
	colMauve,
}

func projectStyle(name string) lipgloss.Style {
	if name == "" || name == "-" {
		return dimStyle
	}
	h := fnv.New32a()
	h.Write([]byte(name))
	return lipgloss.NewStyle().Foreground(projectPalette[h.Sum32()%uint32(len(projectPalette))])
}

// ---------- block-type ----------
func typeGlyph(blockType string) string {
	if blockType == "shallow" {
		return shallowTypeStyle.Render("◐")
	}
	return deepTypeStyle.Render("●")
}

// ---------- open-block header info ----------

type openBlockInfo struct {
	blockNum  int
	outcome   string
	project   string
	startedAt time.Time
}

var (
	headerBarStyle = lipgloss.NewStyle().Bold(true).Foreground(colText).MarginBottom(1)
	blockLenMins   = 90
)
var displayLoc = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.Local
	}
	return loc
}()

type tickMsg time.Time

func tickEvery() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ---------- data rows ----------

type tuiBlock struct {
	id, blockNum int
	date, day    string
	project      string
	outcome      string
	blockType    string
	focus        *float64
	length       string
	closed       bool
}

type tuiGoal struct {
	id, num int
	day     string
	goal    string
	done    bool
	order   int
}
type goalsDay struct {
	name  string // Mon..Sun
	date  string // YYYY-MM-DD
	goals []tuiGoal
}

type goalsWeek struct {
	weekStart string // Monday, YYYY-MM-DD
	weekEnd   string // Sunday, YYYY-MM-DD
	num       int    // 1-based, oldest tracked week = #1
	days      [7]goalsDay
}

var dayOrder = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

func todayDayIndex() int {
	wd := time.Now().Weekday()
	if wd == time.Sunday {
		return 6
	}
	return int(wd) - 1
}

type tuiSleep struct {
	date          string
	weekday       string
	hours         float64
	quality, feel float64
	water         float64
	hasWater      bool
}

type tuiMetric struct {
	project  string
	count    int
	avgFocus float64
}

type tuiProject struct {
	id       int
	name     string
	nextStep string
}

// ---------- generic form ----------
type form struct {
	title  string
	labels []string
	values []string
	field  int
	errMsg string

	ctxID    int    // e.g. block id, goal id, project id being edited
	ctxLabel string // e.g. the project's old name, for a friendly status message
}

func newForm(title string, labels []string, values []string) form {
	if values == nil {
		values = make([]string, len(labels))
	}
	return form{title: title, labels: labels, values: values}
}

func (f *form) handleKey(msg tea.KeyMsg) (submitted bool) {
	switch msg.String() {
	case "backspace":
		if s := f.values[f.field]; len(s) > 0 {
			f.values[f.field] = s[:len(s)-1]
		}
	case "tab", "down":
		f.field = (f.field + 1) % len(f.labels)
	case "alt+backspace":
		f.values[f.field] = deleteLastWord(f.values[f.field])
	case "shift+tab", "up":
		f.field = (f.field - 1 + len(f.labels)) % len(f.labels)
	case "ctrl+j":
		f.values[f.field] += "\n"
	case "enter":
		return true
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			f.values[f.field] += string(msg.Runes)
		}
	}
	return false
}
func deleteLastWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		return s[:i+1]
	}
	return ""
}

var (
	closeFieldLabels       = []string{"Done", "Not done", "Next step", "Files/links", "Focus (1-10)", "Tweak"}
	appendNoteFieldLabels  = []string{"Note"}
	blockUpdateFieldLabels = []string{
		"Outcome", "Context reload",
		"Deliverable", "Done", "Not done",
		"Files/links",
	}
	blockStartFieldLabels    = []string{"Outcome", "Context reload"}
	blockLogFieldLabels      = []string{"Outcome", "Description", "Hours"}
	shallowUpdateFieldLabels = []string{"Outcome", "Description"}
	sleepFieldLabels         = []string{"Hours", "Quality (1-10)", "Feel (1-10)", "Water (L)", "Day"}
	goalAddFieldLabels       = []string{"Goal", "Day"}
	goalEditFieldLabels      = []string{"Goal"}
	projectAddFieldLabels    = []string{"Name"}
	projectRenameFieldLabels = []string{"New name"}
)

// ---------- delete confirmation ----------

type pendingDelete struct {
	kind    string // "block" | "goal" | "sleep" | "project"
	id      int    // for block / goal / project
	dateKey string // for sleep (daily_checkin is keyed by date, not id)
	label   string // human-readable description shown in the confirm prompt
}

// ---------- block detail (read-only) ----------

type tuiBlockDetail struct {
	id, blockNum int
	date, day    string

	project, outcome, contextReload string

	deliverable, doneNotes, notDoneNotes string
	nextStep, filesLinks, tweak          string
	blockType                            string

	focus *float64

	createdAt string
	closedAt  *string
}

// ---------- model ----------

var tuiDB *sql.DB

// Run starts the TUI using the provided database connection.
func Run(db *sql.DB) error {
	tuiDB = db
	m, err := newTUIModel()
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
