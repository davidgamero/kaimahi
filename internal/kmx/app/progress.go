package app

import (
	"fmt"
	"time"
)

// phase identifies one durable unit in a longer command. Native command
// output remains between the start and finish lines, so failures retain all of
// their original diagnostics while the overall journey stays easy to scan.
type phase struct {
	current int
	total   int
	name    string
}

func (a *App) timeNow() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *App) runPhase(p phase, fn func() error) error {
	started := a.timeNow()
	fmt.Fprintf(a.Err, "\nPHASE  [%d/%d] %s\n", p.current, p.total, p.name)
	err := fn()
	elapsed := a.timeNow().Sub(started)
	if err != nil {
		fmt.Fprintf(a.Err, "FAILED [%d/%d] %s (%s)\n", p.current, p.total, p.name, formatElapsed(elapsed))
		return err
	}
	fmt.Fprintf(a.Err, "DONE   [%d/%d] %s (%s)\n", p.current, p.total, p.name, formatElapsed(elapsed))
	return nil
}

func (a *App) complete(label string, started time.Time) {
	fmt.Fprintf(a.Err, "\nCOMPLETE  %s (%s total)\n", label, formatElapsed(a.timeNow().Sub(started)))
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed < time.Second {
		return elapsed.Round(10 * time.Millisecond).String()
	}
	if elapsed < time.Minute {
		return elapsed.Round(100 * time.Millisecond).String()
	}
	return elapsed.Round(time.Second).String()
}
