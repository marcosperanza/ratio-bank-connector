// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"
	"time"
)

// TickerSource says which tickers to download. The file source is the
// standalone one; a Ratio-backed source (sealed watchlist, see the protocol
// doc) plugs in here without touching the engine.
type TickerSource interface {
	Tickers(ctx context.Context) ([]string, error)
}

// FileSource reads one ticker per line; '#' starts a comment.
type FileSource struct{ Path string }

func (f FileSource) Tickers(context.Context) ([]string, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		if t := strings.ToUpper(strings.TrimSpace(line)); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, sc.Err()
}

// Slot configures one provider in fallback order.
type Slot struct {
	Provider   Provider
	DailyLimit int           // 0 = unlimited
	MinGap     time.Duration // pause between calls to the same provider
}

type Engine struct {
	Slots []Slot
	Store Store
	State *State
	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error
	// Log receives one line per ticker outcome.
	Log func(format string, args ...any)
	// OnRun, when set, receives the totals of every RunLoop pass.
	OnRun func(total, failed int, err error)
	// OnOutcomes, when set, receives the outcome of every ticker once a Run is over: this is where the prices
	// and the problems are handed to Ratio.
	OnOutcomes func(ctx context.Context, outcomes []Outcome)

	lastCall map[string]time.Time
}

type Outcome struct {
	Ticker   string
	Provider string
	Symbol   string
	Added    int
	Err      error
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) logf(format string, args ...any) {
	if e.Log != nil {
		e.Log(format, args...)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (e *Engine) pause(ctx context.Context, s Slot) error {
	name := s.Provider.Name()
	if e.lastCall == nil {
		e.lastCall = map[string]time.Time{}
	}
	if wait := s.MinGap - e.now().Sub(e.lastCall[name]); wait > 0 && !e.lastCall[name].IsZero() {
		sl := e.Sleep
		if sl == nil {
			sl = sleepCtx
		}
		if err := sl(ctx, wait); err != nil {
			return err
		}
	}
	e.lastCall[name] = e.now()
	return nil
}

// Run downloads every ticker once. A ticker is tried on each provider in
// order and each provider with every spelling candidate: not-found moves to
// the next spelling, quota or key errors move to the next provider and
// disable this one for the rest of the run.
func (e *Engine) Run(ctx context.Context, tickers []string) []Outcome {
	disabled := map[string]bool{}
	var out []Outcome
	for _, raw := range tickers {
		if ctx.Err() != nil {
			break
		}
		out = append(out, e.runTicker(ctx, ParseTicker(raw), disabled))
	}
	_ = e.State.Save()
	if e.OnOutcomes != nil {
		e.OnOutcomes(ctx, out)
	}
	return out
}

func (e *Engine) runTicker(ctx context.Context, t Ticker, disabled map[string]bool) Outcome {
	res := Outcome{Ticker: t.Raw, Err: ErrNotFound}
	since := e.Store.LastDate(t.Raw)
	for _, slot := range e.Slots {
		name := slot.Provider.Name()
		if disabled[name] {
			continue
		}
		cands := slot.Provider.Candidates(t)
		if len(cands) == 0 {
			if res.Err == ErrNotFound {
				res.Err = ErrUnsupported
			}
			continue
		}
		for _, c := range cands {
			if slot.DailyLimit > 0 && e.State.used(name, e.now()) >= slot.DailyLimit {
				disabled[name] = true
				break
			}
			if err := e.pause(ctx, slot); err != nil {
				res.Err = err
				return res
			}
			e.State.count(name, e.now())
			bars, err := slot.Provider.Fetch(ctx, c, since)
			switch {
			case err == nil:
				added, werr := e.Store.Merge(t.Raw, bars)
				if werr != nil {
					return Outcome{Ticker: t.Raw, Provider: name, Err: werr}
				}
				e.State.learn(t.Raw, name, c.String())
				e.State.Last[t.Raw] = today(e.now())
				e.logf("%s: %s via %s (%s), %d new", t.Raw, "ok", name, c, added)
				return Outcome{Ticker: t.Raw, Provider: name, Symbol: c.String(), Added: added}
			case errors.Is(err, ErrNotFound):
				res.Err = ErrNotFound
				continue
			case errors.Is(err, ErrQuota):
				e.State.exhaust(name, slot.DailyLimit, e.now())
				disabled[name] = true
				e.logf("%s: quota reached, skipping for this run", name)
			case errors.Is(err, ErrAuth):
				disabled[name] = true
				e.logf("%s: API key rejected, skipping for this run", name)
			default:
				res.Err = err
				e.logf("%s: %s via %s: %v", t.Raw, c, name, err)
			}
			break
		}
	}
	e.logf("%s: no provider returned data (%v)", t.Raw, res.Err)
	return res
}

// RunLoop downloads once immediately, then every interval until ctx ends.
// It is meant to run beside the bank daemon: a failure here never touches
// bank syncs, and the reverse.
func (e *Engine) RunLoop(ctx context.Context, src TickerSource, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	for {
		tickers, err := src.Tickers(ctx)
		if err != nil {
			e.logf("prices: cannot read tickers: %v", err)
			if e.OnRun != nil {
				e.OnRun(0, 0, err)
			}
		} else {
			failed := 0
			for _, o := range e.Run(ctx, tickers) {
				if o.Err != nil {
					failed++
				}
			}
			e.logf("prices: %d tickers, %d without data", len(tickers), failed)
			if e.OnRun != nil {
				e.OnRun(len(tickers), failed, nil)
			}
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return
		}
	}
}
