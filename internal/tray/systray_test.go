package tray

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// isolateConfig prevents tests from overwriting the real config file.
func isolateConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("AppData", tmp)
}

func TestNew(t *testing.T) {
	isolateConfig(t)
	tray := New("")
	assert.NotNil(t, tray)
	assert.NotNil(t, tray.config)
	assert.NotNil(t, tray.ticker)
}

func TestTray_onError(t *testing.T) {
	isolateConfig(t)
	tray := New("")

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	go func() {
		select {
		case msg := <-tray.bus:
			err, ok := msg.(error)
			assert.True(t, ok)
			assert.Error(t, err)
		case <-ctx.Done():
			return
		}
	}()

	select {
	case tray.bus <- context.DeadlineExceeded:
	case <-ctx.Done():
	}
	assert.NoError(t, ctx.Err())
}
