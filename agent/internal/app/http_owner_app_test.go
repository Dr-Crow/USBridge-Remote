package app

import (
	"errors"
	"testing"
)

// A repeated Run must fail before signal registration, GUI or any listener.
func TestAppRunCannotReuseRetiredOwner(t *testing.T) {
	for _, headless := range []bool{true, false} {
		a := &App{}
		a.runStarted.Store(true)
		if err := a.Run(headless, false); !errors.Is(err, errEngineAlreadyRun) {
			t.Fatal("repeated Run accepted", err)
		}
		if a.httpServers != nil || a.runCtx != nil {
			t.Fatal("repeated Run acquired resources")
		}
	}
}
