package content

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hydrogencms/hydrogen/core/store"
)

// ErrPreconditionFailed: the item changed since the state the caller expected; see StateMismatchError.
var ErrPreconditionFailed = errors.New("content: precondition failed")

// StateMismatchError carries the item's current state.
type StateMismatchError struct {
	State     string
	UpdatedAt time.Time
	UpdatedBy string
}

func (e *StateMismatchError) Error() string {
	return fmt.Sprintf("%v: item changed at %s", ErrPreconditionFailed, e.UpdatedAt.Format(time.RFC3339Nano))
}

func (e *StateMismatchError) Is(target error) bool { return target == ErrPreconditionFailed }

// StateOf identifies an item's working state: every draft write, discard and publish changes it. Storage keeps
// milliseconds, so the state does too.
func StateOf(updatedAt time.Time) string {
	return updatedAt.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)
}

type expectedStateKey struct{}

// WithExpectedState makes the item mutations (update, publish, discard, delete) run with a precondition: they
// fail with a StateMismatchError, inside their transaction, unless the item's state is still state.
func WithExpectedState(ctx context.Context, state string) context.Context {
	return context.WithValue(ctx, expectedStateKey{}, state)
}

func (s *Service) checkState(ctx context.Context, it store.Item) error {
	want, ok := ctx.Value(expectedStateKey{}).(string)
	if !ok || want == StateOf(it.UpdatedAt) {
		return nil
	}
	return &StateMismatchError{State: StateOf(it.UpdatedAt), UpdatedAt: it.UpdatedAt.UTC(), UpdatedBy: it.UpdatedBy}
}
