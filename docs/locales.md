# Languages, fallbacks and market overlays

## Languages and channel fallback

`config/channels.yaml` may declare a language registry and, per channel, an ordered `languages` list:

```yaml
languages:
  en: {}
  de: {}
  de-CH: { inherits: de }
channels:
  web_dech: { languages: [de-CH, de, en], match: { host: example.com, prefix: /de-ch } }
```

A channel serves an item in the first of its languages the item is available in. If none matches, an item with
`alwaysAvailable` falls back to its main locale. The delivery response says which locale it served, so a
frontend sees a fallback by comparing it with the channel's first language.

## Market overlays (locale inheritance)

`inherits` makes a language a market overlay of another: `de-CH` is `de` with a few differences. Overlays are
switched on by the `overlays` module (`h.Use(overlays.Module())`). Without it `inherits` is ignored, every locale
is independent, and `config:check` and start-up print a warning naming the languages.

How it works:

- An overlay locale stores only the translatable fields it overrides. A field that is present (any value,
  including an empty string or list) overrides; a field that is absent is inherited. Blocks, groups, lists and rich
  text are overridden whole. Shared (non-translatable) fields are not affected.
- Chains work: `de-ZH` can inherit `de-CH`, which inherits `de`. Cycles are refused by the configuration check.
- A published view resolves against the published parent, a draft view against the parent's draft view (its
  draft if it has one, else its published content).
- An overlay locale is available whenever its chain reaches a published locale, even with no document of its own.
  Publishing a locale also rebuilds the delivery snapshots, listing rows, search rows and references of every
  overlay locale below it, in the same transaction, so `de-CH` follows `de` at once. Delivery serves them with
  `locale: "de-CH"`.
- Validation runs on the resolved document: a required field may come from the parent. Publishing an overlay
  locale whose parents do not provide a required field fails for that locale.

### Writing

A draft `PATCH` to an overlay locale stores the fields you send as overrides. Sending `null` for a field removes
its override, so it follows the parent again. Sending the parent's current value is still an override. A write
that leaves a locale with no overrides and no stored document stores nothing.

### Reading (management API)

The item views of an overlay locale carry:

| Field | Meaning |
| --- | --- |
| `fields` | The resolved fields. |
| `inherits` | The direct parent locale. |
| `overrides` | The fields this locale stores (draft or published, as the view). |
| `inheritedFrom` | For each inherited field, the locale it comes from. |

`locales` lists the locales that have a stored document; `availableLocales` adds the overlay locales that are
available through inheritance. Version history, restores, reviews, releases and exports work on the stored
(sparse) documents: restoring a version restores the overrides it had.

### Notes

- Content migrations see resolved documents; the result is stored sparse again (a field the migration changed
  stays inherited when the parent's migrated value is the same).
- `BeforePublish` hooks run for the locales that have a stored document, on the resolved document. Overlay
  locales that exist only by inheritance get no hooks of their own; `AfterPublish` hooks run for them with the
  resolved document.
- Publishing, unpublishing or trashing an item reports the overlay locales below the changed ones in the event's
  `locales`.
