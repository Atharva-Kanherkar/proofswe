package core

import "iter"

type CaptureTrigger string

const (
	CaptureTriggerSessionStart CaptureTrigger = "session_start"
	CaptureTriggerSessionEnd   CaptureTrigger = "session_end"
	CaptureTriggerStop         CaptureTrigger = "stop"
	CaptureTriggerFileWatch    CaptureTrigger = "file_watch"
)

type SourceAdapter interface {
	Detect() error
	Enable() error
	Disable() error
	Capture(trigger CaptureTrigger) iter.Seq[NormalizedEvent]
}
