package terminal

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type Progress struct {
	output  io.Writer
	enabled bool
	mu      sync.Mutex
	active  *Spinner
}

func New(output io.Writer, isTTY, noColor bool) *Progress {
	return &Progress{output: output, enabled: isTTY && !noColor}
}

func Error(output io.Writer, message string, styled bool) {
	if styled {
		fmt.Fprintf(output, "\x1b[31m\u2717\x1b[0m bgr: %s\n", message)
		return
	}
	fmt.Fprintf(output, "bgr: %s\n", message)
}

func (p *Progress) Enabled() bool { return p.enabled }

func (p *Progress) Logf(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
	if p.enabled {
		fmt.Fprintf(p.output, "  \x1b[38;5;63m\u25c6\x1b[0m \x1b[2m%s\x1b[0m\n", fmt.Sprintf(format, args...))
	} else {
		fmt.Fprintf(p.output, "  "+format+"\n", args...)
	}
	p.drawLocked()
}

func (p *Progress) Provider(name, model, reasoning string) {
	if !p.enabled {
		fmt.Fprintf(p.output, "  provider: %q / model: %q", name, model)
		if reasoning != "" {
			fmt.Fprintf(p.output, " / reasoning: %q", reasoning)
		}
		fmt.Fprintln(p.output)
		return
	}
	value := name + " / " + model
	if reasoning != "" {
		value += " / " + reasoning
	}
	fmt.Fprintf(p.output, "  \x1b[38;5;63m\u25c6\x1b[0m \x1b[2mprovider\x1b[0m   \x1b[1m%s\x1b[0m\n", value)
}

func (p *Progress) Warning(message string) {
	if !p.enabled {
		fmt.Fprintf(p.output, "  warning: %s\n", message)
		return
	}
	fmt.Fprintf(p.output, "  \x1b[33m\u26a0\x1b[0m %s\n", message)
}

func (p *Progress) Successf(format string, args ...any) {
	if !p.enabled {
		fmt.Fprintf(p.output, "  "+format+"\n", args...)
		return
	}
	fmt.Fprintf(p.output, "  \x1b[32m\u2713\x1b[0m %s\n", fmt.Sprintf(format, args...))
}

func FormatDuration(duration time.Duration) string { return elapsed(duration) }

func (p *Progress) Wrote(path string, opened bool) {
	if !p.enabled {
		fmt.Fprintf(p.output, "\n  wrote %s\n", path)
		return
	}
	openedText := ""
	if opened {
		openedText = "  \u2192 opened in browser"
	}
	fmt.Fprintf(p.output, "\n  \x1b[32m\u2713\x1b[0m \x1b[2mwrote\x1b[0m      \x1b[1m%s\x1b[0m%s\n", path, openedText)
}

func (p *Progress) Start(message string) *Spinner {
	spinner := &Spinner{parent: p, message: message, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	if !p.enabled {
		close(spinner.done)
		return spinner
	}
	p.mu.Lock()
	p.active = spinner
	p.drawLocked()
	p.mu.Unlock()
	go spinner.loop()
	return spinner
}

func (p *Progress) clearLocked() {
	if p.enabled && p.active != nil {
		fmt.Fprint(p.output, "\r\x1b[2K")
	}
}

func (p *Progress) drawLocked() {
	if !p.enabled || p.active == nil {
		return
	}
	spinner := p.active
	frame := spinnerFrames[spinner.frame%len(spinnerFrames)]
	fmt.Fprintf(p.output, "\r\x1b[2K  \x1b[38;5;63m%s\x1b[0m %s  \x1b[2m%s\x1b[0m", frame, spinner.message, elapsed(time.Since(spinner.started)))
}

type Spinner struct {
	parent  *Progress
	message string
	started time.Time
	frame   int
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (s *Spinner) Update(message string) {
	if !s.parent.enabled {
		return
	}
	s.parent.mu.Lock()
	s.message = message
	s.parent.clearLocked()
	s.parent.drawLocked()
	s.parent.mu.Unlock()
}

func (s *Spinner) Stop() time.Duration {
	duration := time.Since(s.started)
	s.once.Do(func() {
		if s.parent.enabled {
			close(s.stop)
			<-s.done
			s.parent.mu.Lock()
			s.parent.clearLocked()
			s.parent.active = nil
			s.parent.mu.Unlock()
		}
	})
	return duration
}

func (s *Spinner) loop() {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.parent.mu.Lock()
			s.frame++
			s.parent.clearLocked()
			s.parent.drawLocked()
			s.parent.mu.Unlock()
		}
	}
}

func elapsed(duration time.Duration) string {
	if duration < time.Second {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	minutes := int(duration / time.Minute)
	seconds := int(duration/time.Second) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

var spinnerFrames = []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}
