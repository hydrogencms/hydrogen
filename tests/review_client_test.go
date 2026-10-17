package tests

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/hydrogencms/hydrogen/api/client"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

func TestReviewGoClient(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		ctx := context.Background()
		srv := httptest.NewServer(n.h)
		t.Cleanup(srv.Close)
		alice := n.person("alice", append(editorRights, "review.approve")...)
		root := client.NewManage(srv.URL, kernel.DevManageToken)
		art := n.create("article", "", "en", `{"title":"Client"}`, false)
		id := art.String()

		var ce *client.Error
		if _, err := client.NewManage(srv.URL, alice.secret).SubmitReview(ctx, id, "go", "nonsense"); !errors.As(err, &ce) || ce.Status != 400 {
			t.Fatalf("bad reviewer: %v", err)
		}
		rv, err := client.NewManage(srv.URL, alice.secret).SubmitReview(ctx, id, "go")
		if err != nil || rv.State != "in_review" || !rv.Required || len(rv.Events) != 1 || rv.SubmittedByName != "alice" {
			t.Fatalf("submit: %+v %v", rv, err)
		}
		if _, err := client.NewManage(srv.URL, alice.secret).ApproveReview(ctx, id, ""); !errors.As(err, &ce) || ce.Status != 403 || ce.Code != "four_eyes" {
			t.Fatalf("four eyes: %v", err)
		}
		list, err := root.Reviews(ctx, &client.ReviewListOptions{States: []string{"in_review"}, Type: "article"})
		if err != nil || list.Total != 1 || list.Items[0].Item != id || list.Items[0].SubmittedByName != "alice" {
			t.Fatalf("queue: %+v %v", list, err)
		}
		if rv, err = root.CommentReview(ctx, id, "hm"); err != nil || len(rv.Events) != 2 {
			t.Fatalf("comment: %+v %v", rv, err)
		}
		if rv, err = root.RequestReviewChanges(ctx, id, "fix"); err != nil || rv.State != "changes_requested" {
			t.Fatalf("changes: %+v %v", rv, err)
		}
		if _, err = root.SubmitReview(ctx, id, ""); err != nil {
			t.Fatal(err)
		}
		if rv, err = root.WithdrawReview(ctx, id, ""); err != nil || rv.State != "none" {
			t.Fatalf("withdraw: %+v %v", rv, err)
		}
		if _, err = root.SubmitReview(ctx, id, ""); err != nil {
			t.Fatal(err)
		}
		if rv, err = root.Review(ctx, id); err != nil || rv.DraftHash == "" {
			t.Fatalf("draftHash: %+v %v", rv, err)
		}
		if _, err = root.ApproveReviewDraft(ctx, id, "", "stale"); !errors.As(err, &ce) || ce.Status != 409 || ce.Code != "review_stale" {
			t.Fatalf("stale approve: %v", err)
		}
		if aps, err := client.NewManage(srv.URL, alice.secret).ReviewApprovers(ctx, id); err != nil || aps == nil {
			t.Fatalf("approvers: %v %v", aps, err)
		}
		if rv, err = root.ApproveReview(ctx, id, "ok"); err != nil || rv.State != "approved" {
			t.Fatalf("approve: %+v %v", rv, err)
		}
		if rv, err = root.Review(ctx, id); err != nil || rv.State != "approved" {
			t.Fatalf("read: %+v %v", rv, err)
		}
		if _, err := root.Publish(ctx, id, nil); err != nil {
			t.Fatalf("publish: %v", err)
		}
	})
}
