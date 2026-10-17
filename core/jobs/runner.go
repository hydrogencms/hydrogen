package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/store"
)

// ErrLocked is returned by RunNow when another node (or an earlier run) holds the job's lock.
var ErrLocked = errors.New("jobs: job is running elsewhere")

// ErrUnknown is returned by RunNow for a job that isn't registered.
var ErrUnknown = errors.New("jobs: unknown job")

// Job is a named function run on a schedule.
type Job struct {
	Name     string
	Schedule string // see Parse
	Run      func(ctx context.Context) error
}

type Options struct {
	Clock  clock.Clock // default clock.System
	Locker store.Locker
	Log    *slog.Logger // default slog.Default()
	// Poll is how often the runner re-reads the clock while waiting for the next job; it bounds how late a
	// job starts and makes a manual test clock work. Default 1s.
	Poll time.Duration
	// Hold keeps a job's lock after the run, at most half the schedule's interval, so a node whose clock is
	// slightly behind doesn't run the same tick again once the first node finishes. Default 2s.
	Hold time.Duration
}

type entry struct {
	Job
	sched Schedule
	next  time.Time
}

// Runner starts jobs when due. Each run happens under the store lock "job:<name>", so across nodes sharing a
// database a tick runs once; a node that finds the lock held skips the tick.
type Runner struct {
	o     Options
	jobs  []*entry
	runs  sync.WaitGroup
	holds sync.WaitGroup
}

// NewRunner validates the jobs (unique names, valid schedules).
func NewRunner(o Options, jobs []Job) (*Runner, error) {
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Poll <= 0 {
		o.Poll = time.Second
	}
	if o.Hold <= 0 {
		o.Hold = 2 * time.Second
	}
	r := &Runner{o: o}
	seen := map[string]bool{}
	for _, j := range jobs {
		if err := ValidateName(j.Name); err != nil {
			return nil, err
		}
		if seen[j.Name] {
			return nil, fmt.Errorf("jobs: job %q registered twice", j.Name)
		}
		seen[j.Name] = true
		if j.Run == nil {
			return nil, fmt.Errorf("jobs: job %q has no function", j.Name)
		}
		s, err := Parse(j.Schedule)
		if err != nil {
			return nil, fmt.Errorf("jobs: job %q: %w", j.Name, err)
		}
		r.jobs = append(r.jobs, &entry{Job: j, sched: s})
	}
	slices.SortFunc(r.jobs, func(a, b *entry) int { return strings.Compare(a.Name, b.Name) })
	now := o.Clock.Now()
	for _, j := range r.jobs {
		j.next = j.sched.Next(now)
	}
	return r, nil
}

// ValidateName checks a job name: lowercase letters, digits, dashes, underscores, dots and colons.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("jobs: job name is empty")
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9' && i > 0, strings.ContainsRune("-_.:", c) && i > 0:
		default:
			return fmt.Errorf("jobs: job name %q: use lowercase letters, digits, dashes, underscores, dots and colons", name)
		}
	}
	return nil
}

// Names lists the registered jobs, sorted.
func (r *Runner) Names() []string {
	out := make([]string, len(r.jobs))
	for i, j := range r.jobs {
		out[i] = j.Name
	}
	return out
}

// Run starts due jobs until ctx ends, then waits for running jobs.
func (r *Runner) Run(ctx context.Context) error {
	if len(r.jobs) == 0 {
		<-ctx.Done()
		return nil
	}
	defer r.holds.Wait()
	defer r.runs.Wait()
	for {
		r.dispatch(ctx)
		timer := time.NewTimer(r.wait())
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// Tick starts the jobs due at the clock's time and waits for those runs to finish (not for lock holds). Run calls the same logic
// on a timer; tests call Tick after moving a manual clock.
func (r *Runner) Tick(ctx context.Context) {
	r.dispatch(ctx)
	r.runs.Wait()
}

// wait is the time until the next due job, at most Poll.
func (r *Runner) wait() time.Duration {
	now := r.o.Clock.Now()
	d := r.o.Poll
	for _, j := range r.jobs {
		d = min(d, j.next.Sub(now))
	}
	return max(d, time.Millisecond)
}

func (r *Runner) dispatch(ctx context.Context) {
	now := r.o.Clock.Now()
	for _, j := range r.jobs {
		if j.next.After(now) {
			continue
		}
		// A late wake-up runs once, not once per missed tick.
		next := j.sched.Next(now)
		hold := min(r.o.Hold, next.Sub(now)/2)
		j.next = next
		r.runs.Go(func() { r.runLocked(ctx, j, hold) })
	}
}

func (r *Runner) runLocked(ctx context.Context, j *entry, hold time.Duration) {
	lock, ok, err := r.o.Locker.TryLock(ctx, "job:"+j.Name)
	if err != nil {
		if ctx.Err() == nil {
			r.o.Log.Error("jobs: lock failed", "job", j.Name, "err", err)
		}
		return
	}
	if !ok {
		r.o.Log.Debug("jobs: skipped, another node runs it", "job", j.Name)
		return
	}
	if err := r.call(ctx, j); err != nil && ctx.Err() == nil {
		r.o.Log.Error("jobs: job failed", "job", j.Name, "err", err)
	}
	r.holds.Go(func() {
		t := time.NewTimer(hold)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
		release(lock)
	})
}

func release(l store.Lock) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = l.Release(ctx)
}

func (r *Runner) call(ctx context.Context, j *entry) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return j.Run(ctx)
}

// RunNow runs a job immediately under its lock, regardless of its schedule.
func (r *Runner) RunNow(ctx context.Context, name string) error {
	for _, j := range r.jobs {
		if j.Name != name {
			continue
		}
		lock, ok, err := r.o.Locker.TryLock(ctx, "job:"+name)
		if err != nil {
			return fmt.Errorf("jobs: lock %s: %w", name, err)
		}
		if !ok {
			return fmt.Errorf("%w: %s", ErrLocked, name)
		}
		defer release(lock)
		if err := r.call(ctx, j); err != nil {
			return fmt.Errorf("jobs: %s: %w", name, err)
		}
		return nil
	}
	return fmt.Errorf("%w: %s (have: %s)", ErrUnknown, name, strings.Join(r.Names(), ", "))
}
