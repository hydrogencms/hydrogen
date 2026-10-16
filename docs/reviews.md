# Reviews

A content type can require that a draft is **reviewed before it is published**. It is opt-in per type: without
the option nothing changes.

```yaml
types:
  article:
    review: true
    fields: { title: { type: string } }
```

`GET /types` and `GET /types/{type}` report `review: true` for such a type. With it, publishing the item's draft
needs an **approved** review, through the API, the admin, a [release](releases.md) or a schedule, unless the
publisher holds `review.bypass`.

## States

One review per item, about its current draft:

```
none ──submit──▶ in_review ──approve──▶ approved
                    │  ▲                   │
                    │  └──resubmit── changes_requested ◀──request changes (from in_review)
                    └──withdraw──▶ none   (also from approved and changes_requested)
```

| State | Meaning |
| --- | --- |
| `none` | no review under way (never submitted, withdrawn, or closed) |
| `in_review` | submitted, waiting for a decision |
| `approved` | a reviewer approved exactly this draft |
| `changes_requested` | sent back with a comment; the editor edits and submits again |

- **Edits** keep `in_review` and `changes_requested`. Any draft change after `approved` (an edit, a restored
  version, a change of the item's key or availability, a reference removed because its target was purged, an
  unpublish, a content migration) puts the review back to `in_review` (thread event `reopened`, actor = the
  editor, `system` for code). The approval records a hash of the draft parts, the key and the availability;
  publishing re-checks it. An edit while `in_review` adds an `edited` event, once per run of edits by the same
  actor (autosave does not flood the thread).
- **Publishing** the draft, **discarding** it, or moving the item to the **trash** closes the review (`none`). The
  thread stays as history. This holds for a **partial** publish (some locales) and a partial discard too: the whole
  review closes, so submit again for the parts that are left.
- **Key and availability** of a *published* item go live at once, so changing them is a publish: it needs an
  approved review of exactly that draft, or `review.bypass` (audited as `item.update` with
  `data.reviewBypassed: true`). Since the change itself alters the approved hash, in practice that means
  `review.bypass`. On an unpublished item it is an ordinary draft change.
- **Nothing gets stuck**: `Submit` also accepts an `approved` review whose stored hash no longer matches the draft
  (it becomes `in_review`, event `resubmitted`), and **withdraw** works from `in_review`, `approved` and
  `changes_requested`, for the principal who submitted and for anyone with `review.approve`.
- **Four eyes**: nobody who had a hand in the draft may approve it, unless they hold `review.bypass`: the
  principal who submitted it, and every principal who last wrote any of its parts (shared and per locale; not
  only the newest edit). A token or agent counts as the principal that created it, both ways: a user cannot
  approve what their token submitted or edited, and a token cannot approve what its creator did. Tokens created
  by code (no principal) have no creator. Requesting changes is not restricted. Turn four eyes off with
  `content.review_four_eyes: false` in `app.yaml`.
- **Stale approvals**: the review view carries `draftHash`, the hash of the draft as it is now. `approve` takes an
  optional `{draftHash}`; when it differs from the current draft (someone edited after the reviewer opened it)
  the call fails with 409 `review_stale` and nothing changes. Send the hash the reviewer looked at; without it
  the approval applies to whatever the draft is.
- **Reviewers**: an optional list on submit (`user:<id>`, `group:<id>`). It is informational and drives the
  "assigned to me" queue; anyone with `review.approve` (and read access to the item) may decide.
- **Thread** events: `submitted`, `resubmitted`, `approved`, `changes_requested`, `withdrawn`, `reopened`,
  `edited`, `comment`, `published`, `discarded` (also for a trash; its `data.reason` is `trashed`). Comments are at most
  4000 characters; `changes_requested` needs one.

## Policies

| Policy | Allows | Built-in roles |
| --- | --- | --- |
| `review.submit` | submit a draft, comment | `editor`, `contributor` |
| `review.approve` | approve, request changes, withdraw anyone's submission, comment | `editor` |
| `review.bypass` | publish without an approved review; approve your own work | `administrator` (through `*`) |

They take the same limitations as the content policies (collection, owner, channel), and a type's code access
rules apply as for `content.edit` (submit) and `content.publish` (approve, bypass). Roles created before
upgrading keep their policies: grant the new ones where wanted. A principal that cannot read an item gets 404.

A publish that uses `review.bypass` while the review would have refused it is audited as `item.publish` with
`data.reviewBypassed: true`. Code that runs without a principal (the system) is not checked and may publish.

## API

Under `/api/manage/v1`. Names (`submittedByName`, `actorName`, …) are resolved like `createdByName`.

| Route | Needs | Answer |
| --- | --- | --- |
| `GET /items/{id}/review` | read access | the review (below); a type without `review` answers `state: none`, `required: false` |
| `POST /items/{id}/review/submit` `{comment?, reviewers?}` | `review.submit` | the review |
| `POST /items/{id}/review/approve` `{comment?, draftHash?}` | `review.approve` | the review |
| `POST /items/{id}/review/request-changes` `{comment}` | `review.approve` | the review |
| `POST /items/{id}/review/withdraw` `{comment?}` | the submitter, or `review.approve` | the review |
| `POST /items/{id}/review/comments` `{comment}` | `review.submit` or `review.approve` | the review |
| `GET /reviews?state=&type=&reviewer=me&limit=&offset=` | `content.read` | the queue |
| `GET /reviews/approvers?item=<id>` | `review.submit` on the item | `{items: [{ref, name, kind}]}` |

```json
{ "state": "in_review", "required": true,
  "submittedBy": "token:…", "submittedByName": "alice", "submittedAt": "2026-10-14T09:00:00Z",
  "reviewers": ["user:…", "group:…"], "reviewerNames": { "user:…": "Rita", "group:…": "Desk" },
  "decidedBy": "", "decidedByName": "", "decidedAt": null,
  "canSubmit": false, "canApprove": true, "canWithdraw": false, "draftHash": "9f2c…",
  "events": [ { "id": "…", "kind": "submitted", "comment": "please look", "actor": "token:…",
                "actorName": "alice", "at": "2026-10-14T09:00:00Z" } ] }
```

`canApprove` already takes four eyes into account. `events` lists the thread oldest first (the latest 200).

The queue lists `{items, total, hasMore}` newest submission first; a row is `{item, type, name, state,
submittedBy, submittedByName, submittedAt, reviewers, reviewerNames, decidedBy, decidedByName, decidedAt,
updatedAt}` (`reviewerNames` maps each reviewer ref to its name, like the item view). Without `state` it shows
`in_review`, `changes_requested` and `approved` (comma-separated values select several, `none` included).
`reviewer=me` keeps the reviews that name the caller or one of the caller's groups. Only items the caller can read
appear. The scan stops one readable row past the page, so `hasMore` is exact while `total` is exact only when
`hasMore` is false (otherwise it is `offset + limit + 1`, a lower bound): page with `hasMore`. Review types the
caller has no read grant for are left out in the query itself.

`GET /reviews/approvers?item=` lists the users and groups that could approve the item, sorted by name, without the
caller: those holding `review.approve` (and `content.read`) through their roles, directly, or through a group,
with the item's type, collection and rule limitations applied; disabled users are left out. The admin's reviewer
picker uses it. Four eyes is not applied (it depends on the draft).

Errors: 409 `review_state` for a move the state does not allow (including submitting without a draft), 409 `review_stale`
for an approval of an outdated `draftHash`, 403
`four_eyes`, 403 `forbidden` for a missing policy, 400 for a type without `review`, a missing required comment
or a bad reviewer, 409 **`review_required`** from publish (`POST /items/{id}/publish`, singletons too).

In a release, the item's error code is `review_required`, and nothing publishes. A scheduled release publishes as
the principal who scheduled it, so it needs the approval (or their `review.bypass`) when it runs.

## Events

Outbox topics `review.submitted` (submissions and resubmissions), `review.approved`, `review.changes_requested`
and `review.withdrawn`, each with `{item, type, state, actor, reviewers}`; the same names are audit actions on
the item. Subscribe a webhook to `review.*` to notify reviewers. Publishing, discarding and reopening are in the
thread and in the item's own `item.*` events.

## Go client

`client.Manage` has `Review`, `SubmitReview`, `ApproveReview`, `RequestReviewChanges`, `WithdrawReview`,
`CommentReview`, `Reviews`, `ApproveReviewDraft` (with the `draftHash`) and `ReviewApprovers`.
