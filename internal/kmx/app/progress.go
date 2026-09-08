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
	if a.enhancedProgress {
		fmt.Fprintf(a.Err, "\n%s==> QUICKSTART %d/%d%s  %s\n", a.progressANSI("\033[1;36m"), p.current, p.total, a.progressANSI("\033[0m"), p.name)
	} else {
		fmt.Fprintf(a.Err, "\nPHASE  [%d/%d] %s\n", p.current, p.total, p.name)
	}
	err := fn()
	elapsed := a.timeNow().Sub(started)
	if err != nil {
		if a.enhancedProgress {
			fmt.Fprintf(a.Err, "%s[failed %d/%d]%s %s (%s)\n", a.progressANSI("\033[1;31m"), p.current, p.total, a.progressANSI("\033[0m"), p.name, formatElapsed(elapsed))
		} else {
			fmt.Fprintf(a.Err, "FAILED [%d/%d] %s (%s)\n", p.current, p.total, p.name, formatElapsed(elapsed))
		}
		return err
	}
	if a.enhancedProgress {
		fmt.Fprintf(a.Err, "%s[done %d/%d]%s   %s (%s)\n", a.progressANSI("\033[1;32m"), p.current, p.total, a.progressANSI("\033[0m"), p.name, formatElapsed(elapsed))
	} else {
		fmt.Fprintf(a.Err, "DONE   [%d/%d] %s (%s)\n", p.current, p.total, p.name, formatElapsed(elapsed))
	}
	return nil
}

func (a *App) progressANSI(code string) string {
	if a.progressColor {
		return code
	}
	return ""
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
