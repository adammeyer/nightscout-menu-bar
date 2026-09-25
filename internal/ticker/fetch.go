package ticker

import (
	"context"
	"log/slog"
	"time"

	"gabe565.com/nightscout-menu-bar/internal/nightscout"
)

// minInterval prevents polling fast enough to trigger LibreLinkUp rate limits.
const minInterval = 30 * time.Second

func (t *Ticker) beginFetch(ctx context.Context, render chan<- *nightscout.Properties) {
	go func() {
		t.fetchTicker = time.NewTicker(t.interval())
		defer t.fetchTicker.Stop()

		for {
			next := t.Fetch(ctx, render)
			t.fetchTicker.Reset(next)
			slog.Debug("Scheduled next fetch", "in", next)

			select {
			case <-ctx.Done():
				return
			case <-t.fetchTicker.C:
			}
		}
	}()
}

func (t *Ticker) Fetch(ctx context.Context, render chan<- *nightscout.Properties) time.Duration {
	properties, err := t.fetch.Do(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return t.interval()
		}
		t.bus <- err
	}
	if properties != nil {
		if render != nil {
			render <- properties
		}
		if t.config.Data().Socket.Enabled {
			t.socket.Write(properties)
		}
	}
	return t.interval()
}

func (t *Ticker) interval() time.Duration {
	return max(t.config.Data().Advanced.Interval.Duration, minInterval)
}
