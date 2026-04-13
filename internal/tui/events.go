// Package tui renders braid execution via bubbletea. The executor emits
// typed events on a channel; the bubbletea Program consumes them and
// updates the model.
package tui

import "time"

// Event is the sealed interface implemented by every event type emitted
// by the executor. Events are also tea.Msg values — bubbletea's Update
// receives them directly via tea.Program.Send.
type Event interface {
	eventMarker()
}

// PhaseEvent marks a top-level phase (e.g. iteration boundary, repeat pass,
// ralph task). The TUI renders Title as a prominent banner.
type PhaseEvent struct {
	Title string
}

// StepEvent marks the start of a single step (work/review/gate/iterate/ralph).
// Agent/Model/Iteration/MaxIterations are for display.
// RunIndex is non-zero for events inside a composition branch (1-based).
type StepEvent struct {
	Step          string
	Agent         string
	Model         string
	Iteration     int
	MaxIterations int
	RunIndex      int
}

// LineEvent is one streamed stdout line from the active agent.
// RunIndex identifies which parallel branch emitted it (0 for single-run).
type LineEvent struct {
	Text     string
	RunIndex int
}

// PromptEvent carries the fully-rendered prompt being sent to the agent.
// The TUI shows this in a collapsible panel when ShowRequest is true.
type PromptEvent struct {
	Text string
}

// WaitingEvent fires when a rate-limit retry is scheduled.
type WaitingEvent struct {
	NextRetryAt time.Time
	Attempt     int
	Err         string
}

// RetryEvent fires at the start of a retry attempt.
type RetryEvent struct {
	Attempt int
}

// GateEvent reports the gate verdict after each iteration.
type GateEvent struct {
	Verdict  string // "DONE" | "ITERATE" | "MAX_ITERATIONS" | "NEXT"
	Message  string
	RunIndex int
}

// LogEvent is a generic informational line (warnings, status notes that
// don't fit a more specific event).
type LogEvent struct {
	Level string // "info" | "warn" | "error"
	Text  string
}

// DoneEvent signals the executor has finished the entire run.
// The TUI should unblock its Run() and exit after this.
type DoneEvent struct {
	LastMessage string
	LogFile     string
	Verdict     string
	Iterations  int
}

// ErrorEvent is emitted when the executor fails fatally.
type ErrorEvent struct {
	Err string
}

func (PhaseEvent) eventMarker()   {}
func (StepEvent) eventMarker()    {}
func (LineEvent) eventMarker()    {}
func (PromptEvent) eventMarker()  {}
func (WaitingEvent) eventMarker() {}
func (RetryEvent) eventMarker()   {}
func (GateEvent) eventMarker()    {}
func (LogEvent) eventMarker()     {}
func (DoneEvent) eventMarker()    {}
func (ErrorEvent) eventMarker()   {}

// Emitter wraps a channel send to make nil-channel checks local.
// Handlers that may or may not have a TUI attached use this to avoid
// peppering their code with `if ec.Events != nil` guards.
type Emitter chan<- Event

// Send delivers e if the channel is non-nil; otherwise it's a no-op.
// The send is blocking so that bursts of streaming output (e.g. agent
// stdout during a long ralph task) backpressure the executor instead of
// being silently dropped. The channel is buffered for smoothing; the TUI
// pumper drains it into bubbletea as fast as it can render.
func (e Emitter) Send(ev Event) {
	if e == nil {
		return
	}
	e <- ev
}
