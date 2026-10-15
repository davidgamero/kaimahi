package app

import "time"

// SuiteProgress carries metadata only. It is an observation, not a receipt or
// permission to run the next stage. Sinks must be bounded and non-blocking.
type SuiteProgress struct {
	Stage, Object, State string
	At                   time.Time
}

func (a *App) suiteProgress(stage, object, state string) {
	if a.suiteEvents != nil {
		a.suiteEvents(SuiteProgress{stage, object, state, time.Now()})
	}
}
