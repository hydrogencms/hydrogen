package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/features/review"
)

func TestReviewTypeOption(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		types := n.call(root, "GET", "/types", nil, 200)
		got := map[string]any{}
		var list []any
		if err := jsonDecode(types.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		for _, x := range list {
			m := x.(map[string]any)
			got[m["name"].(string)] = m["review"]
		}
		deq(t, "article", got["article"], true)
		deq(t, "note", got["note"], false)
		one := n.call(root, "GET", "/types/article", nil, 200).json()
		deq(t, "type review", field(one, "type", "review"), true)
		// An item of a type without review: nothing is required.
		note := n.create("note", "", "en", `{"title":"N"}`, false)
		rv := n.review(note)
		deq(t, "required", rv["required"], false)
		deq(t, "state", rv["state"], "none")
		n.act(root, note, "submit", nil, 400)
		n.publish(note)
	})
}

func TestReviewTransitions(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		alice := n.person("alice", append(editorRights, "review.approve")...)
		bob := n.person("bob", reviewerRights...)
		art := n.create("article", "", "en", `{"title":"Draft"}`, false)

		rv := n.review(art)
		deq(t, "initial state", rv["state"], "none")
		deq(t, "required", rv["required"], true)
		deq(t, "admin canSubmit", rv["canSubmit"], true)

		// Submit, with a reviewer; submitting again is a state error.
		n.act(alice, art, "submit", map[string]any{"comment": " please look ", "reviewers": []string{"user:" + ids.UUID{}.String()}}, 400)
		rv = n.act(alice, art, "submit", map[string]any{"comment": " please look "}, 200)
		deq(t, "state", rv["state"], "in_review")
		deq(t, "submittedBy", rv["submittedBy"], alice.actor)
		deq(t, "submittedByName", rv["submittedByName"], "alice")
		deq(t, "canWithdraw", rv["canWithdraw"], true)
		deq(t, "alice cannot approve her own", rv["canApprove"], false)
		deq(t, "kinds", strings.Join(kinds(rv), ","), "submitted")
		deq(t, "comment trimmed", rv["events"].([]any)[0].(map[string]any)["comment"], "please look")
		deq(t, "second submit", errCode(n.act(alice, art, "submit", nil, 409)), "review_state")

		// Changes requested needs a comment; the editor resubmits.
		n.act(bob, art, "request-changes", map[string]any{}, 400)
		rv = n.act(bob, art, "request-changes", map[string]any{"comment": "typo in the title"}, 200)
		deq(t, "state", rv["state"], "changes_requested")
		deq(t, "decidedByName", rv["decidedByName"], "bob")
		deq(t, "alice can submit again", n.call(alice, "GET", "/items/"+art.String()+"/review", nil, 200).json()["canSubmit"], true)
		deq(t, "approve in wrong state", errCode(n.act(bob, art, "approve", nil, 409)), "review_state")
		n.edit(alice, art, `{"title":"Draft fixed"}`)
		deq(t, "an edit keeps changes_requested", n.state(art), "changes_requested")
		rv = n.act(alice, art, "submit", nil, 200)
		deq(t, "state", rv["state"], "in_review")
		rv = n.act(bob, art, "approve", map[string]any{"comment": "ok"}, 200)
		deq(t, "state", rv["state"], "approved")
		deq(t, "decidedBy", rv["decidedBy"], bob.actor)
		deq(t, "kinds", strings.Join(kinds(rv), ","), "submitted,changes_requested,resubmitted,approved")
		deq(t, "approved cannot be approved again", errCode(n.act(bob, art, "approve", nil, 409)), "review_state")

		// Comments keep the state.
		n.act(bob, art, "comments", map[string]any{}, 400)
		n.act(bob, art, "comments", map[string]any{"comment": strings.Repeat("x", 4001)}, 400)
		rv = n.act(bob, art, "comments", map[string]any{"comment": "nice"}, 200)
		deq(t, "state after comment", rv["state"], "approved")
		deq(t, "kinds", kinds(rv)[len(kinds(rv))-1], "comment")

		// Withdraw: the submitter, and an approver; others cannot.
		art2 := n.create("article", "", "en", `{"title":"Two"}`, false)
		n.act(alice, art2, "submit", nil, 200)
		other := n.person("other", "content.read", "review.submit")
		n.act(other, art2, "withdraw", nil, 403)
		rv = n.act(alice, art2, "withdraw", map[string]any{"comment": "not ready"}, 200)
		deq(t, "state", rv["state"], "none")
		deq(t, "kinds", strings.Join(kinds(rv), ","), "submitted,withdrawn")
		n.act(alice, art2, "submit", nil, 200)
		deq(t, "approver withdraws", n.act(bob, art2, "withdraw", nil, 200)["state"], "none")
		deq(t, "withdraw when none", errCode(n.act(alice, art2, "withdraw", nil, 409)), "review_state")

		// Nothing to review without a draft: a published item with no changes.
		pub := n.create("article", "", "en", `{"title":"Live"}`, true)
		deq(t, "no draft", errCode(n.act(alice, pub, "submit", nil, 409)), "review_state")

		n.drain()
		deq(t, "topics", strings.Join(n.probe.topics(), ","),
			"review.submitted,review.changes_requested,review.submitted,review.approved,review.submitted,review.withdrawn,review.submitted,review.withdrawn")
		p := n.probe.last("review.approved")
		deq(t, "payload item", p["item"], art.String())
		deq(t, "payload type", p["type"], "article")
		deq(t, "payload state", p["state"], "approved")
		deq(t, "payload actor", p["actor"], bob.actor)
		deq(t, "audit", len(n.auditData(art, "review.*")), 4)
	})
}

func TestReviewReviewers(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		s := n.app.Services()
		u, err := s.Admin.CreateUser(ctx, access.UserInput{Login: "rita", Name: "Rita"})
		if err != nil {
			t.Fatal(err)
		}
		g, err := s.Admin.CreateGroup(ctx, "Editors", "")
		if err != nil {
			t.Fatal(err)
		}
		art := n.create("article", "", "en", `{"title":"A"}`, false)
		rv := n.act(root, art, "submit", map[string]any{"reviewers": []string{"user:" + u.ID.String(), "group:" + g.ID.String(), "user:" + u.ID.String()}}, 200)
		deq(t, "reviewers", asJSON(rv["reviewers"]), `["user:`+u.ID.String()+`","group:`+g.ID.String()+`"]`)
		deq(t, "name", field(rv, "reviewerNames", "user:"+u.ID.String()), "Rita")
		deq(t, "group name", field(rv, "reviewerNames", "group:"+g.ID.String()), "Editors")
		n.act(root, art, "withdraw", nil, 200)
		n.act(root, art, "submit", map[string]any{"reviewers": []string{"nonsense"}}, 400)
		n.act(root, art, "submit", map[string]any{"reviewers": []string{"user:" + ids.UUID{1}.String()}}, 400)
	})
}

func TestReviewFourEyes(t *testing.T) {
	for _, on := range []bool{true, false} {
		extra := ""
		if !on {
			extra = ", review_four_eyes: false"
		}
		name := "on"
		if !on {
			name = "off"
		}
		t.Run(name, func(t *testing.T) {
			runReview(t, extra, func(t *testing.T, n *revNode) {
				alice := n.person("alice", append(editorRights, "review.approve")...)
				bob := n.person("bob", append(reviewerRights, "content.edit")...)
				carol := n.person("carol", append(reviewerRights, "review.bypass")...)
				blocked := func(w who, id ids.UUID) {
					t.Helper()
					if on {
						deq(t, w.name+" blocked", errCode(n.act(w, id, "approve", nil, 403)), "four_eyes")
					}
				}

				// The submitter cannot approve.
				a := n.create("article", "", "en", `{"title":"A"}`, false)
				n.act(alice, a, "submit", nil, 200)
				blocked(alice, a)
				if !on {
					n.act(alice, a, "approve", nil, 200)
					return
				}
				deq(t, "canApprove hint", n.call(alice, "GET", "/items/"+a.String()+"/review", nil, 200).json()["canApprove"], false)
				deq(t, "bob can", n.call(bob, "GET", "/items/"+a.String()+"/review", nil, 200).json()["canApprove"], true)

				// The last editor cannot either, even when someone else submitted.
				n.edit(bob, a, `{"title":"A by bob"}`)
				blocked(bob, a)
				n.act(carol, a, "approve", nil, 200) // bypass holders may approve their own or anyone's
				// Reviewers still can request changes on their own edits: it harms nothing.
				b := n.create("article", "", "en", `{"title":"B"}`, false)
				n.act(alice, b, "submit", nil, 200)
				n.edit(bob, b, `{"title":"B2"}`)
				n.act(bob, b, "request-changes", map[string]any{"comment": "no"}, 200)

				// The administrator (every right) submits and approves its own.
				c := n.create("article", "", "en", `{"title":"C"}`, false)
				n.act(root, c, "submit", nil, 200)
				deq(t, "admin approves own", n.act(root, c, "approve", nil, 200)["state"], "approved")
			})
		})
	}
}

func TestReviewPublishGate(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		alice := n.person("alice", append(editorRights, "review.approve")...)
		bob := n.person("bob", reviewerRights...)
		publish := func(w who, id ids.UUID, want int) map[string]any {
			t.Helper()
			return n.call(w, "POST", "/items/"+id.String()+"/publish", nil, want).json()
		}
		art := n.create("article", "", "en", `{"title":"Gate"}`, false)

		// Not submitted, in review, changes requested: refused with review_required.
		deq(t, "unsubmitted", errCode(publish(alice, art, 409)), "review_required")
		n.act(alice, art, "submit", nil, 200)
		deq(t, "in review", errCode(publish(alice, art, 409)), "review_required")
		n.act(bob, art, "request-changes", map[string]any{"comment": "no"}, 200)
		deq(t, "changes requested", errCode(publish(alice, art, 409)), "review_required")
		n.act(alice, art, "submit", nil, 200)
		n.act(bob, art, "approve", nil, 200)

		// An edit after the approval reopens the review (actor = the editor) and blocks the publish.
		n.edit(alice, art, `{"title":"Gate edited"}`)
		rv := n.review(art)
		deq(t, "reopened", rv["state"], "in_review")
		last := rv["events"].([]any)[len(rv["events"].([]any))-1].(map[string]any)
		deq(t, "event", last["kind"], "reopened")
		deq(t, "event actor", last["actor"], alice.actor)
		deq(t, "event actor name", last["actorName"], "alice")
		deq(t, "after reopen", errCode(publish(alice, art, 409)), "review_required")
		n.act(bob, art, "approve", nil, 200)

		// An approved draft publishes; the review closes and the thread says so.
		publish(alice, art, 200)
		rv = n.review(art)
		deq(t, "closed", rv["state"], "none")
		ks := kinds(rv)
		deq(t, "published event", ks[len(ks)-1], "published")
		for _, d := range n.auditData(art, "item.publish") {
			if _, ok := d["reviewBypassed"]; ok {
				t.Errorf("an approved publish is flagged as bypassed: %v", d)
			}
		}

		// A bypass holder publishes an unreviewed draft; the audit entry says so.
		n.edit(alice, art, `{"title":"Hotfix"}`)
		deq(t, "no bypass", errCode(publish(alice, art, 409)), "review_required")
		boss := n.person("boss", append(editorRights, "review.bypass")...)
		publish(boss, art, 200)
		flagged := 0
		for _, d := range n.auditData(art, "item.publish") {
			if d["reviewBypassed"] == true {
				flagged++
			}
		}
		deq(t, "bypass flagged", flagged, 1)
		// The administrator holds every right, bypass included.
		n.edit(alice, art, `{"title":"Again"}`)
		publish(root, art, 200)

		// The approval covers exactly the draft it saw: a draft changed behind the hook's back is refused.
		sneaky := n.create("article", "", "en", `{"title":"Sneaky"}`, false)
		n.act(alice, sneaky, "submit", nil, 200)
		n.act(bob, sneaky, "approve", nil, 200)
		err := n.app.Services().DB.Tx(ctx, func(tx store.Tx) error {
			parts, err := tx.Content().GetDraftParts(ctx, sneaky)
			if err != nil {
				return err
			}
			p := parts[0]
			p.Data = []byte(`{"title":"Changed behind the back"}`)
			return tx.Content().PutDraftPart(ctx, p)
		})
		if err != nil {
			t.Fatal(err)
		}
		deq(t, "tampered", errCode(publish(alice, sneaky, 409)), "review_required")

		// Types without review publish as before.
		note := n.create("note", "", "en", `{"title":"N"}`, false)
		n.call(alice, "POST", "/items/"+note.String()+"/publish", nil, 200)
	})
}

func TestReviewGateInReleasesAndSchedules(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		alice := n.person("alice", append(editorRights, "release.manage", "review.approve")...)
		bob := n.person("bob", reviewerRights...)
		a := n.create("article", "", "en", `{"title":"A"}`, false)
		b := n.create("article", "", "en", `{"title":"B"}`, false)
		note := n.create("note", "", "en", `{"title":"N"}`, false)

		items := []map[string]any{ritem(a, "publish"), ritem(b, "publish"), ritem(note, "publish")}
		rel := n.call(alice, "POST", "/releases", map[string]any{"name": "R", "items": items}, 201).json()["id"].(string)
		r := n.call(alice, "POST", "/releases/"+rel+"/publish", nil, 422).json()
		errs := field(r, "release", "errors").([]any)
		var codes []string
		for _, e := range errs {
			m := e.(map[string]any)
			codes = append(codes, m["item"].(string)+":"+m["code"].(string))
		}
		deq(t, "errors", strings.Join(codes, ","), minmax(a.String()+":review_required", b.String()+":review_required"))
		if n.liveItem(note) || n.liveItem(a) {
			t.Fatal("something published although the release failed")
		}

		// Approve one: the other still blocks. Then the release goes.
		n.act(alice, a, "submit", nil, 200)
		n.act(bob, a, "approve", nil, 200)
		r = n.call(alice, "POST", "/releases/"+rel+"/publish", nil, 422).json()
		deq(t, "one left", len(field(r, "release", "errors").([]any)), 1)
		n.act(alice, b, "submit", nil, 200)
		n.act(bob, b, "approve", nil, 200)
		n.call(alice, "POST", "/releases/"+rel+"/publish", nil, 200)
		if !n.liveItem(a) || !n.liveItem(b) || !n.liveItem(note) {
			t.Fatal("not published")
		}
		deq(t, "closed", n.state(a), "none")

		// A scheduled publish runs as its scheduler and fails the same way; the one-item kind too.
		c := n.create("article", "", "en", `{"title":"C"}`, false)
		sched := n.call(alice, "POST", "/releases", map[string]any{"name": "S", "kind": "item", "items": []map[string]any{ritem(c, "publish")}}, 201).json()["id"].(string)
		n.call(alice, "POST", "/releases/"+sched+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 200)
		n.clk.Advance(2 * time.Hour)
		if ran, err := n.app.Services().Releases.RunDue(ctx); err != nil || ran != 1 {
			t.Fatalf("RunDue = %d, %v", ran, err)
		}
		got := n.call(root, "GET", "/releases/"+sched, nil, 200).json()
		deq(t, "status", got["status"], "failed")
		deq(t, "code", got["errors"].([]any)[0].(map[string]any)["code"], "review_required")
		if n.liveItem(c) {
			t.Fatal("published unreviewed by the schedule")
		}
		// Approved in time: the same release publishes when it runs again.
		n.act(alice, c, "submit", nil, 200)
		n.act(bob, c, "approve", nil, 200)
		n.call(alice, "POST", "/releases/"+sched+"/publish", nil, 200)
		if !n.liveItem(c) {
			t.Fatal("approved item not published")
		}
	})
}

func (n *revNode) liveItem(id ids.UUID) bool {
	n.t.Helper()
	n.drain()
	return n.get("/items/"+id.String()+"?channel=web").Code == 200
}

func minmax(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "," + b
}

func TestReviewClosesOnDiscardAndTrash(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		alice := n.person("alice", append(editorRights, "content.delete")...)
		a := n.create("article", "", "en", `{"title":"A"}`, true)
		n.edit(alice, a, `{"title":"A2"}`)
		n.act(alice, a, "submit", nil, 200)
		if err := n.app.Services().Content.Discard(alice.ctx, a, nil); err != nil {
			t.Fatal(err)
		}
		rv := n.review(a)
		deq(t, "discarded", rv["state"], "none")
		ks := kinds(rv)
		deq(t, "event", ks[len(ks)-1], "discarded")
		// A new draft starts a new review on the same thread.
		n.edit(alice, a, `{"title":"A3"}`)
		n.act(alice, a, "submit", nil, 200)
		deq(t, "thread", strings.Join(kinds(n.review(a)), ","), "submitted,discarded,submitted")

		n.call(alice, "DELETE", "/items/"+a.String(), nil, 204)
		var row store.ItemReview
		err := n.app.Services().DB.View(ctx, func(tx store.Tx) error {
			var err error
			row, err = tx.Reviews().Get(ctx, a)
			return err
		})
		if err != nil || row.State != "none" {
			t.Fatalf("after trash: %+v, %v", row, err)
		}
		evs := []store.ReviewEvent{}
		_ = n.app.Services().DB.View(ctx, func(tx store.Tx) error {
			evs, err = tx.Reviews().Events(ctx, a, 10, ids.Nil)
			return err
		})
		deq(t, "trash event", evs[0].Kind, "discarded")
		deq(t, "trash reason", strings.ReplaceAll(string(evs[0].Data), " ", ""), `{"reason":"trashed"}`)
		// The trashed item is gone for the review API; restored, it can start over.
		n.call(root, "GET", "/items/"+a.String()+"/review", nil, 404)
		n.call(alice, "POST", "/items/"+a.String()+"/restore", nil, 200)
		n.act(alice, a, "submit", nil, 200)
	})
}

func TestReviewQueue(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		s := n.app.Services()
		alice := n.person("alice", append(editorRights, "review.approve")...)
		bob := n.person("bob", reviewerRights...)
		users := map[string]access.User{}
		for _, l := range []string{"uma", "vic"} {
			u, err := s.Admin.CreateUser(ctx, access.UserInput{Login: l, Name: strings.ToUpper(l[:1]) + l[1:]})
			if err != nil {
				t.Fatal(err)
			}
			users[l] = u
		}
		grp, err := s.Admin.CreateGroup(ctx, "Desk", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Admin.AddMember(ctx, grp.ID, users["vic"].ID); err != nil {
			t.Fatal(err)
		}
		role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: "readers", Policies: []access.RolePolicy{rolePolicy("content.read", nil)}})
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range users {
			if _, err := s.Admin.CreateAssignment(ctx, access.AssignmentInput{RoleID: role.ID, Principal: store.PrincipalRef{Kind: "user", ID: u.ID}}); err != nil {
				t.Fatal(err)
			}
		}
		userCtx := func(l string) *access.Principal {
			p, err := s.Auth.PrincipalOf(ctx, "user:"+users[l].ID.String())
			if err != nil || p == nil {
				t.Fatal(l, err)
			}
			return p
		}

		a := n.create("article", "", "en", `{"title":"Alpha"}`, false)
		b := n.create("article", "", "en", `{"title":"Beta"}`, false)
		c := n.create("article", "", "en", `{"title":"Gamma"}`, false)
		n.tick()
		n.act(alice, a, "submit", map[string]any{"reviewers": []string{"user:" + users["uma"].ID.String()}}, 200)
		n.tick()
		n.act(alice, b, "submit", map[string]any{"reviewers": []string{"group:" + grp.ID.String()}}, 200)
		n.tick()
		n.act(alice, c, "submit", nil, 200)
		n.act(bob, c, "approve", nil, 200)

		rows := func(w who, q string) []map[string]any {
			t.Helper()
			r := n.call(w, "GET", "/reviews"+q, nil, 200).json()
			var out []map[string]any
			for _, x := range r["items"].([]any) {
				out = append(out, x.(map[string]any))
			}
			if q == "" || strings.HasPrefix(q, "?type") || strings.HasPrefix(q, "?state=approved") {
				deq(t, "total "+q, r["total"], float64(len(out)))
			}
			return out
		}
		names := func(rs []map[string]any) string {
			var out []string
			for _, r := range rs {
				out = append(out, r["name"].(string)+":"+r["state"].(string))
			}
			return strings.Join(out, ",")
		}
		deq(t, "default", names(rows(root, "")), "Gamma:approved,Beta:in_review,Alpha:in_review")
		deq(t, "state", names(rows(root, "?state=approved")), "Gamma:approved")
		deq(t, "states", names(rows(root, "?state=in_review,approved&limit=2")), "Gamma:approved,Beta:in_review")
		deq(t, "page", names(rows(root, "?limit=1&offset=1")), "Beta:in_review")
		deq(t, "type", names(rows(root, "?type=person")), "")
		deq(t, "type article", len(rows(root, "?type=article")), 3)
		n.call(root, "GET", "/reviews?state=bogus", nil, 400)
		n.call(root, "GET", "/reviews?reviewer=you", nil, 400)
		first := rows(root, "")[0]
		deq(t, "item", first["item"], c.String())
		deq(t, "submittedByName", first["submittedByName"], "alice")
		deq(t, "decidedByName", first["decidedByName"], "bob")
		deq(t, "type", first["type"], "article")

		// reviewer=me: a named user, and a member of a named group; a token has no reviewer ref.
		q := func(l string) []string {
			t.Helper()
			rs, _, _, err := s.Review.Queue(access.WithPrincipal(ctx, userCtx(l)), review.QueueOptions{Me: true})
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, r := range rs {
				out = append(out, r.Name)
			}
			return out
		}
		deq(t, "uma", strings.Join(q("uma"), ","), "Alpha")
		deq(t, "vic through the group", strings.Join(q("vic"), ","), "Beta")
		deq(t, "token", len(rows(alice, "?reviewer=me")), 0)
		deq(t, "admin", len(rows(root, "?reviewer=me")), 0)

		// Items the principal may not read are left out, and trashed ones too.
		person := n.create("person", "", "en", `{"fullName":"P"}`, true)
		_ = person
		onlyNotes := n.person("notes", "content.read")
		role2, _ := s.Admin.RoleByName(ctx, "notes")
		keep := []access.RolePolicy{rolePolicy("content.read", map[string][]string{"collection": {"note"}})}
		if _, err := s.Admin.UpdateRole(ctx, role2.ID, access.RolePatch{Policies: &keep}); err != nil {
			t.Fatal(err)
		}
		deq(t, "no read", len(rows(onlyNotes, "")), 0)
		n.call(root, "DELETE", "/items/"+a.String(), nil, 204)
		deq(t, "trashed left out", names(rows(root, "")), "Gamma:approved,Beta:in_review")
	})
}

func TestReviewAccess(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		art := n.create("article", "", "en", `{"title":"A"}`, false)
		viewer := n.person("viewer", "content.read")
		blind := n.person("blind", "review.submit")
		contributor := n.person("contrib", "content.read", "content.edit", "review.submit")
		reviewer := n.person("rev", reviewerRights...)

		n.call(viewer, "GET", "/items/"+art.String()+"/review", nil, 200)
		n.act(viewer, art, "submit", nil, 403)
		n.act(viewer, art, "comments", map[string]any{"comment": "x"}, 403)
		n.act(viewer, art, "approve", nil, 403)
		n.act(blind, art, "submit", nil, 404) // cannot read the item: it does not exist for them
		n.call(blind, "GET", "/items/"+art.String()+"/review", nil, 403)
		n.act(contributor, art, "submit", nil, 200)
		n.act(contributor, art, "approve", nil, 403)
		n.act(contributor, art, "request-changes", map[string]any{"comment": "c"}, 403)
		n.act(reviewer, art, "submit", nil, 403)
		deq(t, "contributor comments", len(kinds(n.act(contributor, art, "comments", map[string]any{"comment": "hi"}, 200))), 2)
		deq(t, "reviewer comments", len(kinds(n.act(reviewer, art, "comments", map[string]any{"comment": "ho"}, 200))), 3)
		// A viewer cannot withdraw someone else's submission.
		n.act(viewer, art, "withdraw", nil, 403)
		n.call(viewer, "GET", "/reviews", nil, 200)
		n.call(blind, "GET", "/reviews", nil, 403)
		n.call(root, "GET", "/items/"+ids.UUID{9}.String()+"/review", nil, 404)
		n.call(root, "GET", "/items/nope/review", nil, 404)

		// Limitations apply: submit limited to another type cannot touch articles.
		limited := n.person("limited", "content.read")
		role, _ := n.app.Services().Admin.RoleByName(ctx, "limited")
		pols := []access.RolePolicy{rolePolicy("content.read", nil), rolePolicy("review.submit", map[string][]string{"collection": {"note"}})}
		if _, err := n.app.Services().Admin.UpdateRole(ctx, role.ID, access.RolePatch{Policies: &pols}); err != nil {
			t.Fatal(err)
		}
		art2 := n.create("article", "", "en", `{"title":"B"}`, false)
		n.act(limited, art2, "submit", nil, 403)

		// Templates: editor submits and approves, contributor submits, administrator everything, nobody else bypasses.
		have := map[string]string{}
		for _, tpl := range access.Templates() {
			var ps []string
			for _, p := range tpl.Policies {
				if strings.HasPrefix(p.Policy, "review.") || p.Policy == "*" {
					ps = append(ps, p.Policy)
				}
			}
			have[tpl.Name] = strings.Join(ps, ",")
		}
		deq(t, "templates", asJSON(have), `{"administrator":"*","ai-contributor":"","contributor":"review.submit","editor":"review.submit,review.approve","viewer":""}`)
	})
}

func TestReviewRulesFollowTheItem(t *testing.T) {
	// Without access control (a service with no principal) the workflow still runs as the system.
	runReview(t, "", func(t *testing.T, n *revNode) {
		art := n.create("article", "", "en", `{"title":"Sys"}`, false)
		if _, err := n.svc().Submit(ctx, art, review.SubmitInput{Comment: "from code"}); err != nil {
			t.Fatal(err)
		}
		if _, err := n.svc().Approve(ctx, art, review.ApproveInput{}); err != nil {
			t.Fatal(err)
		}
		if _, err := n.app.Services().Content.Publish(ctx, art, content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
		deq(t, "closed", n.state(art), "none")
	})
}
