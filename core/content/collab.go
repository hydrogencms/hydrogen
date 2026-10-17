package content

import "context"

type collabClientKey struct{}

// WithCollabClient marks the writes made with ctx as made by a collaboration client (the id the admin's editor
// holds in a co-editing session): item.updated, item.published and discard events carry it as collabClient, so
// the collaboration service can tell a participant's save from any other change of the draft.
func WithCollabClient(ctx context.Context, client string) context.Context {
	return context.WithValue(ctx, collabClientKey{}, client)
}

// CollabClientFrom returns the client set by WithCollabClient, or "".
func CollabClientFrom(ctx context.Context) string {
	c, _ := ctx.Value(collabClientKey{}).(string)
	return c
}

type collabSeqKey struct{}

// WithCollabSeq records the highest update seq the collaboration client had applied when it saved (the
// X-Hydrogen-Collab-Seq header); the "saved" notification carries it so that other clients can tell whether the
// save includes everything they applied.
func WithCollabSeq(ctx context.Context, seq int64) context.Context {
	return context.WithValue(ctx, collabSeqKey{}, seq)
}

// CollabSeqFrom returns the seq set by WithCollabSeq, or 0.
func CollabSeqFrom(ctx context.Context) int64 {
	n, _ := ctx.Value(collabSeqKey{}).(int64)
	return n
}
