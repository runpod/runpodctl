package waitfor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeClock lets the wait-loop tests run instantly: Sleep advances the clock
// instead of blocking, so a "10 minute" timeout costs microseconds.
type fakeClock struct {
	now    time.Time
	slept  []time.Duration
	cancel error // returned by Sleep to simulate ctrl-c
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) sleepFunc() func(context.Context, time.Duration) error {
	return func(ctx context.Context, d time.Duration) error {
		c.slept = append(c.slept, d)
		if c.cancel != nil {
			return c.cancel
		}
		c.now = c.now.Add(d)
		return nil
	}
}

func TestUntil(t *testing.T) {
	cases := []struct {
		name        string
		polls       []State
		pollErr     error
		cancelAfter int // when > 0, Sleep returns context.Canceled from this sleep on
		timeout     time.Duration
		interval    time.Duration
		wantPolls   int
		wantCode    string
		wantErr     string
		wantLast    string
	}{
		{
			name:      "ready on the first poll does not sleep",
			polls:     []State{{Ready: true, Detail: "ssh reachable at 1.2.3.4:22"}},
			timeout:   time.Minute,
			interval:  5 * time.Second,
			wantPolls: 1,
		},
		{
			name: "keeps polling until ready",
			polls: []State{
				{Detail: "pod not listed yet"},
				{Detail: "ssh port not allocated yet"},
				{Ready: true},
			},
			timeout:   time.Minute,
			interval:  5 * time.Second,
			wantPolls: 3,
		},
		{
			name:     "times out with the last known state and a stable code",
			polls:    []State{{Detail: "ssh port 1.2.3.4:51227 allocated but not reachable: connection refused"}},
			timeout:  12 * time.Second,
			interval: 5 * time.Second,
			// 0s, 5s, 10s, then a last poll at the 12s deadline before giving up.
			wantPolls: 4,
			wantCode:  CodeTimeout,
			wantErr:   "timed out after 12s waiting for ssh on pod abc123",
			wantLast:  "connection refused",
		},
		{
			name:      "a poll error aborts and keeps the underlying error",
			polls:     []State{{Detail: "ignored"}},
			pollErr:   errors.New("unauthorized"),
			timeout:   time.Minute,
			wantPolls: 1,
			wantErr:   "waiting for ssh on pod abc123: unauthorized",
		},
		{
			name: "cancellation reports wait_interrupted, not a timeout",
			polls: []State{
				{Detail: "ssh port not allocated yet"},
			},
			cancelAfter: 1,
			timeout:     time.Minute,
			interval:    5 * time.Second,
			wantPolls:   1,
			wantCode:    CodeInterrupted,
			wantErr:     "interrupted after 0s waiting for ssh on pod abc123",
			wantLast:    "ssh port not allocated yet",
		},
		{
			name:      "an empty detail still reports something",
			polls:     []State{{}},
			timeout:   time.Second,
			interval:  time.Second,
			wantPolls: 2,
			wantCode:  CodeTimeout,
			wantLast:  "not ready",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newFakeClock()
			if tc.cancelAfter > 0 {
				clock.cancel = context.Canceled
			}

			polls := 0
			poll := func(context.Context) (State, error) {
				polls++
				if tc.pollErr != nil {
					return State{}, tc.pollErr
				}
				idx := polls - 1
				if idx >= len(tc.polls) {
					idx = len(tc.polls) - 1
				}
				return tc.polls[idx], nil
			}

			var progress bytes.Buffer
			_, err := Until(context.Background(), poll, Options{
				Label:    "ssh on pod abc123",
				Timeout:  tc.timeout,
				Interval: tc.interval,
				Progress: &progress,
				Now:      clock.Now,
				Sleep:    clock.sleepFunc(),
			})

			if polls != tc.wantPolls {
				t.Errorf("polled %d times, want %d", polls, tc.wantPolls)
			}

			if tc.wantCode == "" && tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.Contains(progress.String(), "ready after") {
					t.Errorf("expected a readiness line on progress, got %q", progress.String())
				}
				return
			}

			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if tc.wantLast != "" && !strings.Contains(err.Error(), tc.wantLast) {
				t.Errorf("error = %q, want the last state %q", err.Error(), tc.wantLast)
			}

			var waitErr *Error
			if tc.wantCode != "" {
				if !errors.As(err, &waitErr) {
					t.Fatalf("expected a *waitfor.Error, got %#v", err)
				}
				if waitErr.ErrorCode() != tc.wantCode {
					t.Errorf("code = %q, want %q", waitErr.ErrorCode(), tc.wantCode)
				}
			} else if errors.As(err, &waitErr) {
				t.Errorf("poll errors must not be reported as a wait error: %#v", waitErr)
			}
		})
	}
}

// The wait must not sleep past its own deadline: a 12s budget with a 5s interval
// has to shorten the last sleep to 2s rather than overshoot to 15s.
func TestUntilDoesNotSleepPastDeadline(t *testing.T) {
	clock := newFakeClock()
	sleep := clock.sleepFunc()

	_, err := Until(context.Background(), func(context.Context) (State, error) {
		return State{Detail: "not yet"}, nil
	}, Options{
		Label:    "ssh on pod abc123",
		Timeout:  12 * time.Second,
		Interval: 5 * time.Second,
		Now:      clock.Now,
		Sleep:    sleep,
	})
	if err == nil {
		t.Fatal("expected a timeout")
	}

	want := []time.Duration{5 * time.Second, 5 * time.Second, 2 * time.Second}
	if len(clock.slept) != len(want) {
		t.Fatalf("slept %v, want %v", clock.slept, want)
	}
	for i := range want {
		if clock.slept[i] != want[i] {
			t.Fatalf("slept %v, want %v", clock.slept, want)
		}
	}
}

// Progress must be throttled (the resource takes minutes; one line per poll is
// spam) and must never be written when no writer was supplied.
func TestUntilProgressCadence(t *testing.T) {
	clock := newFakeClock()

	var progress bytes.Buffer
	_, err := Until(context.Background(), func(context.Context) (State, error) {
		return State{Detail: "still booting"}, nil
	}, Options{
		Label:         "ssh on pod abc123",
		Timeout:       time.Minute,
		Interval:      5 * time.Second,
		ProgressEvery: 15 * time.Second,
		Progress:      &progress,
		Now:           clock.Now,
		Sleep:         clock.sleepFunc(),
	})
	if err == nil {
		t.Fatal("expected a timeout")
	}

	lines := strings.Count(strings.TrimSpace(progress.String()), "\n") + 1
	// 13 polls over 60s: one opening line plus a note at 15s/30s/45s/60s.
	if lines != 5 {
		t.Fatalf("expected 5 progress lines, got %d: %q", lines, progress.String())
	}
	if !strings.HasPrefix(progress.String(), "waiting for ssh on pod abc123 (timeout 1m0s)") {
		t.Errorf("unexpected first progress line: %q", progress.String())
	}
	if !strings.Contains(progress.String(), "still waiting for ssh on pod abc123 (15s elapsed): still booting") {
		t.Errorf("progress lines must carry elapsed time and the detail: %q", progress.String())
	}
}

func TestUntilWithoutProgressWriterStaysSilent(t *testing.T) {
	clock := newFakeClock()
	if _, err := Until(context.Background(), func(context.Context) (State, error) {
		return State{Ready: true}, nil
	}, Options{Label: "x", Now: clock.Now, Sleep: clock.sleepFunc()}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUntilDefaults(t *testing.T) {
	polls := 0
	_, err := Until(context.Background(), func(context.Context) (State, error) {
		polls++
		return State{Ready: true}, nil
	}, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if polls != 1 {
		t.Fatalf("polled %d times, want 1", polls)
	}
}
