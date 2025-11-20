package main

import (
	"fmt"
	"math"
	"math/cmplx"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gordonklaus/portaudio"
	"github.com/mjibson/go-dsp/fft"
)

const (
	barCount   = 32
	maxHeight  = 15
	sampleRate = 44100
	bufferSize = 1024
)

var (
	appStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#FDBA74")).
			Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFF7ED")).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#FB923C")).
			Padding(0, 1)

	clockStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FDBA74")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#FB923C")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Italic(true)

	colorPalette = []lipgloss.Color{
		lipgloss.Color("#FFF7ED"),
		lipgloss.Color("#FFEDD5"),
		lipgloss.Color("#FED7AA"),
		lipgloss.Color("#FDBA74"),
		lipgloss.Color("#FB923C"),
		lipgloss.Color("#F97316"),
		lipgloss.Color("#EA580C"),
		lipgloss.Color("#C2410C"),
	}
)

type audioMsg []int
type titleMsg string
type tickMsg time.Time

type model struct {
	width       int
	height      int
	mediaTitle  string
	currentTime time.Time
	stream      *portaudio.Stream
	buffer      []float32
	heights     []int
}

func initialModel(stream *portaudio.Stream, buffer []float32) model {
	return model{
		mediaTitle:  "Waiting for media...",
		stream:      stream,
		buffer:      buffer,
		heights:     make([]int, barCount),
		currentTime: time.Now(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		listenAudio(m.stream, m.buffer),
		checkMusic(),
		tickClock(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}

	case audioMsg:
		m.heights = msg
		return m, listenAudio(m.stream, m.buffer)

	case titleMsg:
		m.mediaTitle = string(msg)
		return m, checkMusic()

	case tickMsg:
		m.currentTime = time.Time(msg)
		return m, tickClock()
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	availableWidth := m.width - 6
	availableHeight := m.height - 4

	titleText := titleStyle.Render(m.mediaTitle)
	clockText := clockStyle.Render(m.currentTime.Format("15:04:05"))

	header := lipgloss.JoinHorizontal(lipgloss.Top,
		titleText,
		lipgloss.PlaceHorizontal(availableWidth-lipgloss.Width(titleText), lipgloss.Right, clockText),
	)

	var rightBars []string
	var leftBars []string

	for _, h := range m.heights {
		if h < 0 {
			h = 0
		}
		if h > maxHeight {
			h = maxHeight
		}

		colorIndex := 0
		if maxHeight > 0 {
			colorIndex = (h * len(colorPalette)) / maxHeight
		}
		if colorIndex >= len(colorPalette) {
			colorIndex = len(colorPalette) - 1
		}

		barColor := colorPalette[colorIndex]
		style := lipgloss.NewStyle().Foreground(barColor)

		barStr := strings.Repeat("█\n", h)
		barStr = strings.TrimSuffix(barStr, "\n")

		renderedBar := style.Render(barStr)
		rightBars = append(rightBars, renderedBar)
		leftBars = append([]string{renderedBar}, leftBars...)
	}

	combinedBars := append(leftBars, rightBars...)
	visualizer := lipgloss.JoinHorizontal(lipgloss.Bottom, combinedBars...)

	visualizer = lipgloss.Place(
		availableWidth, availableHeight-4,
		lipgloss.Center, lipgloss.Bottom,
		visualizer,
	)

	footer := footerStyle.Render("Press 'q' to quit")
	footer = lipgloss.PlaceHorizontal(availableWidth, lipgloss.Center, footer)

	content := lipgloss.JoinVertical(lipgloss.Top, header, visualizer, footer)

	return appStyle.
		Width(m.width - 2).
		Height(m.height).
		Render(content)
}

func listenAudio(stream *portaudio.Stream, buffer []float32) tea.Cmd {
	return func() tea.Msg {
		err := stream.Read()
		if err != nil {
			return audioMsg(make([]int, barCount))
		}

		input := make([]float64, len(buffer))
		for i, v := range buffer {
			input[i] = float64(v)
		}

		coeffs := fft.FFTReal(input)
		heights := make([]int, barCount)

		minFreq := 20.0
		maxFreq := 8000.0
		logBase := math.Log(maxFreq / minFreq)

		for i := 0; i < barCount; i++ {
			startFreq := minFreq * math.Exp(logBase*float64(i)/float64(barCount))
			endFreq := minFreq * math.Exp(logBase*float64(i+1)/float64(barCount))

			startIndex := int(startFreq * float64(len(coeffs)) * 2 / sampleRate)
			endIndex := int(endFreq * float64(len(coeffs)) * 2 / sampleRate)

			if startIndex >= len(coeffs) {
				startIndex = len(coeffs) - 1
			}
			if endIndex >= len(coeffs) {
				endIndex = len(coeffs) - 1
			}
			if endIndex <= startIndex {
				endIndex = startIndex + 1
			}

			var magnitude float64
			for j := startIndex; j < endIndex; j++ {
				if j < len(coeffs) {
					magnitude += cmplx.Abs(coeffs[j])
				}
			}

			avg := magnitude / float64(endIndex-startIndex)

			boost := 1.0 + (float64(i) * 0.1)
			heights[i] = int(avg * 40 * boost)
		}

		return audioMsg(heights)
	}
}

func checkMusic() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		var cmd *exec.Cmd
		var out []byte
		var err error

		switch runtime.GOOS {
		case "darwin":
			script := `tell application "System Events"
				set processList to (name of every process)
			end tell
			if processList contains "Spotify" then
				tell application "Spotify" to return name of current track
			else if processList contains "Music" then
				tell application "Music" to return name of current track
			else
				return "No Music App Running"
			end if`
			cmd = exec.Command("osascript", "-e", script)
			out, err = cmd.Output()

		case "linux":
			cmd = exec.Command("playerctl", "metadata", "title")
			out, err = cmd.Output()

		case "windows":
			psScript := `Get-Process | Where-Object {$_.MainWindowTitle -ne ""} | Where-Object {$_.ProcessName -eq "Spotify"} | Select-Object -ExpandProperty MainWindowTitle -First 1`
			cmd = exec.Command("powershell", "-NoProfile", "-Command", psScript)
			out, err = cmd.Output()

		default:
			return titleMsg("OS Not Supported")
		}

		if err != nil || len(out) == 0 {
			return titleMsg("No Media Found")
		}
		return titleMsg(strings.TrimSpace(string(out)))
	})
}

func tickClock() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func main() {
	portaudio.Initialize()
	defer portaudio.Terminate()

	buffer := make([]float32, bufferSize)
	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, bufferSize, buffer)
	if err != nil {
		fmt.Printf("Error opening stream: %v\n", err)
		os.Exit(1)
	}

	stream.Start()
	defer stream.Close()

	p := tea.NewProgram(initialModel(stream, buffer), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
