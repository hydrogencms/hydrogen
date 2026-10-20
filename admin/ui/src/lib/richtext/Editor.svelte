<script lang="ts">
  import { getContext, untrack } from 'svelte';
  import type { Plugin } from 'prosemirror-state';
  import type * as Y from 'yjs';
  import { COLLAB_CTX, type CollabCtx } from '../collab/context';
  import { toggleMark } from 'prosemirror-commands';
  import { redo, undo } from 'prosemirror-history';
  import type { Command } from 'prosemirror-state';
  import { EditorState } from 'prosemirror-state';
  import { EditorView } from 'prosemirror-view';
  import { addColumnAfter, addColumnBefore, addRowAfter, addRowBefore, deleteColumn, deleteRow, deleteTable, isInTable, mergeCells, splitCell, toggleHeaderRow } from 'prosemirror-tables';
  import './richtext.css';
  import { t } from '../i18n';
  import '../messages/richtext';
  import Button from '../ui/Button.svelte';
  import Dialog from '../ui/Dialog.svelte';
  import Icon from '../ui/Icon.svelte';
  import { editorIcons } from './icons';
  import Menu, { type MenuItem } from '../ui/Menu.svelte';
  import Popover from '../ui/Popover.svelte';
  import Textarea from '../ui/Textarea.svelte';
  import { blockOf, inside, insertImage, insertRule, insertTable, link as linkCommands, markActive, plugins, toggleBlock, toggleList, toggleQuote } from './commands';
  import ImageForm from './ImageForm.svelte';
  import LinkForm from './LinkForm.svelte';
  import { buildSchema, fromStored, isEmptyDoc, toStored } from './schema';
  import type { RichTextProps } from './types';

  let {
    value,
    config,
    locale = '',
    readonly = false,
    error,
    label,
    id,
    'aria-describedby': describedby,
    placeholder,
    onchange,
    rows = 8,
    json = true,
    path,
  }: RichTextProps = $props();

  const uid = $props.id();
  /** The ProseMirror view (undefined until built, or when the value cannot be shown); for B4 and tests. */
  export const getView = () => view;

  const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
  const mod = mac ? '⌘' : 'Ctrl+';
  const shift = mac ? '⇧' : 'Shift+';

  // The schema follows the field's allowlists by value: a parent that rebuilds `config` on every render must
  // not rebuild the editor (that would drop the selection).
  const configKey = $derived(JSON.stringify([config?.marks ?? null, config?.nodes ?? null]));
  const schema = $derived.by(() => {
    const [marks, nodes] = JSON.parse(configKey) as [string[] | null, string[] | null];
    try {
      return buildSchema({ marks: marks ?? undefined, nodes: nodes ?? undefined });
    } catch {
      return null;
    }
  });
  const linkApi = $derived(schema ? linkCommands(schema) : null);

  // With a collaboration session the field's Y fragment is the document: y-prosemirror syncs the editor with it
  // (and brings the undo stack), and `value` is only written, never read. The Yjs code loads when a fragment shows up.
  const collab = getContext<CollabCtx | undefined>(COLLAB_CTX);
  const frag = $derived(path ? (collab?.binding?.fragment(path) as Y.XmlFragment | undefined) : undefined);
  let yp = $state.raw<typeof import('y-prosemirror')>();
  $effect(() => {
    if (frag && !yp) import('y-prosemirror').then((m) => (yp = m), () => undefined);
  });
  let boundTo: Y.XmlFragment | undefined;
  const undoCmd: Command = (st, d, v) => (boundTo && yp ? yp.undo(st) : undo(st, d, v));
  const redoCmd: Command = (st, d, v) => (boundTo && yp ? yp.redo(st) : redo(st, d, v));
  // yjs commands act when called, so "can" looks at the stacks instead.
  function stack(st: EditorState, which: 'undo' | 'redo') {
    const um = yp?.yUndoPluginKey.getState(st)?.undoManager;
    return !!um && (which === 'undo' ? um.undoStack : um.redoStack).length > 0;
  }

  let host = $state<HTMLElement>();
  let bar = $state<HTMLElement>();
  let view: EditorView | undefined;
  let tick = $state(0);
  let lastJSON = '';
  let broken = $state(false);
  let linkBtn = $state<HTMLElement>();
  let linkOpen = $state(false);
  let imageOpen = $state(false);
  const extraMarks = [
    ['underline', 'U'],
    ['strike', `${shift}${mod}X`],
    ['superscript', '.'],
    ['subscript', ','],
  ] as const;

  const stamp = (v: unknown) => (v === undefined || v === null || isEmptyDoc(v) ? '' : JSON.stringify(v));

  function attributes() {
    const doc = view?.state.doc;
    const empty = !doc || (doc.childCount === 1 && doc.firstChild!.isTextblock && doc.firstChild!.content.size === 0);
    return {
      class: 'h-rte__doc',
      role: 'textbox',
      'aria-multiline': 'true',
      'aria-label': label,
      ...(id ? { id } : {}),
      ...(describedby ? { 'aria-describedby': describedby } : {}),
      ...(error ? { 'aria-invalid': 'true' } : {}),
      ...(readonly ? { 'aria-readonly': 'true' } : {}),
      'data-placeholder': empty && !readonly ? (placeholder ?? t('richtext.placeholder')) : '',
      spellcheck: 'true',
    } as Record<string, string>;
  }

  function create(doc: ReturnType<typeof fromStored>) {
    return EditorState.create({ doc: doc!, plugins: plugins({ schema: schema!, openLink: () => linkBtn?.querySelector('button')?.click() }) });
  }

  function createBound(sc: NonNullable<typeof schema>, fr: Y.XmlFragment, y: NonNullable<typeof yp>) {
    return EditorState.create({
      schema: sc,
      plugins: plugins({
        schema: sc,
        openLink: () => linkBtn?.querySelector('button')?.click(),
        history: { plugins: [y.ySyncPlugin(fr) as Plugin, y.yUndoPlugin() as Plugin], undo: y.undo as never, redo: y.redo as never },
      }),
    });
  }

  function emit(state: EditorState) {
    const next = isEmptyDoc(state.doc.toJSON()) ? undefined : toStored(state.doc);
    lastJSON = stamp(next);
    onchange?.(next);
  }

  // (Re)build when the host is there; follow outside changes of the value (locale switch, copy from the main locale).
  $effect(() => {
    const el = host;
    const sc = schema;
    if (!el || !sc) return;
    const fr = frag;
    const y = yp;
    if (fr && y) {
      if (view && boundTo === fr && view.state.schema === sc) return;
      view?.destroy();
      boundTo = fr;
      broken = false;
      view = new EditorView(el, {
        state: createBound(sc, fr, y),
        editable: () => !readonly,
        attributes,
        // y-prosemirror dispatches while the view is being built, before `view` is assigned: use `this`.
        dispatchTransaction(this: EditorView, tr) {
          const state = this.state.apply(tr);
          this.updateState(state);
          tick++;
          // Changes that came from the shared document reach the form through the binding.
          if (tr.docChanged && !tr.getMeta(y.ySyncPluginKey)?.isChangeOrigin) emit(state);
        },
      });
      untrack(() => tick++);
      return;
    }
    if (boundTo) {
      view?.destroy();
      view = undefined;
      boundTo = undefined;
    }
    const incoming = value;
    const same = view && stamp(incoming) === lastJSON && view.state.schema === sc;
    if (same) return;
    const doc = fromStored(sc, incoming);
    broken = doc === null;
    if (!doc) return;
    lastJSON = stamp(incoming);
    if (view && view.state.schema === sc) {
      view.updateState(create(doc));
    } else {
      view?.destroy();
      view = new EditorView(el, {
        state: create(doc),
        editable: () => !readonly,
        attributes,
        dispatchTransaction(tr) {
          const state = view!.state.apply(tr);
          view!.updateState(state);
          tick++;
          if (tr.docChanged) emit(state);
        },
      });
    }
    untrack(() => tick++);
  });

  $effect(() => {
    // Props that change attributes or editability on a live view.
    void [label, id, describedby, error, readonly, placeholder, tick];
    view?.setProps({ attributes, editable: () => !readonly });
  });

  $effect(() => () => {
    view?.destroy();
    view = undefined;
  });

  // ---- toolbar state (re-derived after every transaction)
  const s = $derived.by(() => {
    void tick;
    const st = view?.state;
    if (!st || !schema) return null;
    const n = schema.nodes;
    const m = schema.marks;
    const can = (c: Command) => c(st);
    const b = blockOf(st);
    return {
      undo: boundTo ? stack(st, 'undo') : can(undo),
      redo: boundTo ? stack(st, 'redo') : can(redo),
      block: b.type === 'heading' ? `h${b.level}` : b.type === 'codeBlock' ? 'code' : 'p',
      bold: markActive(st, m.bold),
      italic: markActive(st, m.italic),
      code: markActive(st, m.code),
      boldOk: !!m.bold && can(toggleMark(m.bold)),
      italicOk: !!m.italic && can(toggleMark(m.italic)),
      codeOk: !!m.code && can(toggleMark(m.code)),
      extra: Object.fromEntries(extraMarks.map(([k]) => [k, { on: markActive(st, m[k]), ok: !!m[k] && can(toggleMark(m[k])) }])) as Record<string, { on: boolean; ok: boolean }>,
      link: !!linkApi?.hrefAt(st),
      linkOk: !!m.link && !st.selection.$from.parent.type.spec.code,
      bullet: inside(st, n.bulletList),
      ordered: inside(st, n.orderedList),
      quote: inside(st, n.blockquote),
      codeBlock: b.type === 'codeBlock',
      href: linkApi?.hrefAt(st) ?? '',
    };
  });

  function run(c: Command) {
    if (!view) return;
    c(view.state, view.dispatch, view);
    view.focus();
  }

  const hint = (name: string, keys?: string) => (keys ? `${name} (${keys})` : name);
  const n = $derived(schema?.nodes ?? {});
  const m = $derived(schema?.marks ?? {});
  const headings = $derived(n.heading ? [2, 3, 4] : []);

  const tableItems = $derived.by<MenuItem[]>(() => {
    void tick;
    const st = view?.state;
    if (!schema || !n.table) return [];
    const item = (key: string, cmd: Command, separator = false): MenuItem => ({ label: t(`richtext.${key}`), separator, disabled: !st || !cmd(st), onselect: () => run(cmd) });
    return [
      item('tableInsert', insertTable(schema)),
      item('rowBefore', addRowBefore, true),
      item('rowAfter', addRowAfter),
      item('rowDelete', deleteRow),
      item('colBefore', addColumnBefore, true),
      item('colAfter', addColumnAfter),
      item('colDelete', deleteColumn),
      item('cellMerge', mergeCells, true),
      item('cellSplit', splitCell),
      item('headerRow', toggleHeaderRow),
      { ...item('tableDelete', deleteTable, true), danger: true, disabled: !st || !isInTable(st) },
    ];
  });

  const styleItems = $derived.by<MenuItem[]>(() => {
    if (!schema) return [];
    const cur = s?.block;
    const items: MenuItem[] = [];
    if (n.paragraph) items.push({ label: t('richtext.paragraph'), checked: cur === 'p', hint: `${mod}Alt+0`, onselect: () => run(toggleBlock(schema, 'paragraph')) });
    for (const l of headings) {
      items.push({ label: t('richtext.heading', { level: l }), checked: cur === `h${l}`, hint: `${mod}Alt+${l}`, onselect: () => run(toggleBlock(schema, 'heading', { level: l })) });
    }
    return items;
  });
  const styleLabel = $derived.by(() => {
    const cur = s?.block ?? 'p';
    if (cur.startsWith('h') && headings.includes(Number(cur[1]))) return t('richtext.heading', { level: cur[1] });
    if (cur === 'code') return t('richtext.codeBlock');
    return n.paragraph ? t('richtext.paragraph') : t('richtext.heading', { level: cur[1] ?? 2 });
  });

  // ---- roving tabindex on the toolbar
  let rover = $state(0);
  const stops = () => [...(bar?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])];
  $effect(() => {
    void tick;
    void readonly;
    if (!bar) return;
    const all = [...bar.querySelectorAll<HTMLButtonElement>('button')];
    const live = all.filter((b) => !b.disabled);
    const cur = Math.min(rover, live.length - 1);
    for (const b of all) b.tabIndex = b === live[cur] ? 0 : -1;
  });
  function barKey(e: KeyboardEvent) {
    const list = stops();
    const i = list.indexOf(document.activeElement as HTMLButtonElement);
    if (i < 0) return;
    let to = -1;
    if (e.key === 'ArrowRight') to = (i + 1) % list.length;
    else if (e.key === 'ArrowLeft') to = (i - 1 + list.length) % list.length;
    else if (e.key === 'Home') to = 0;
    else if (e.key === 'End') to = list.length - 1;
    else return;
    e.preventDefault();
    rover = to;
    list[to].focus();
  }
  function barFocus(e: FocusEvent) {
    const i = stops().indexOf(e.target as HTMLButtonElement);
    if (i >= 0) rover = i;
  }

  // ---- edit as JSON
  let jsonOpen = $state(false);
  let jsonText = $state('');
  let jsonProblem = $state('');
  function openJson() {
    const cur = view && !broken ? (isEmptyDoc(view.state.doc.toJSON()) ? undefined : toStored(view.state.doc)) : value;
    jsonText = cur === undefined ? '' : JSON.stringify(cur, null, 2);
    jsonProblem = '';
    jsonOpen = true;
  }
  function applyJson() {
    const raw = jsonText.trim();
    let v: unknown;
    if (raw !== '') {
      try {
        v = JSON.parse(raw);
      } catch (e) {
        jsonProblem = t('richtext.jsonInvalid', { message: (e as Error).message });
        return;
      }
      if (typeof v !== 'object' || v === null || (v as { type?: unknown }).type !== 'doc') {
        jsonProblem = t('richtext.jsonNotDoc');
        return;
      }
      if (!schema || fromStored(schema, v) === null) {
        jsonProblem = t('richtext.jsonSchema');
        return;
      }
    }
    jsonOpen = false;
    const doc = schema ? fromStored(schema, v) : null;
    if (view && doc && boundTo) {
      view.dispatch(view.state.tr.replaceWith(0, view.state.doc.content.size, doc.content));
      broken = false;
    } else if (view && doc) {
      view.updateState(create(doc));
      broken = false;
      tick++;
    }
    lastJSON = stamp(v);
    onchange?.(raw === '' || isEmptyDoc(v) ? undefined : v);
  }

  const menu = $derived<MenuItem[]>(json ? [{ label: t('richtext.editJson'), icon: 'code', onselect: openJson }] : []);
</script>

{#snippet ico(name: string)}
  <svg class="h-icon" width="16" height="16" viewBox="0 0 24 24" stroke-width="1.75" aria-hidden="true" focusable="false"><use href="#{uid}-{name}" /></svg>
{/snippet}

<svg class="h-sprite" width="0" height="0" aria-hidden="true" focusable="false">
  <defs>
    {#each Object.entries(editorIcons) as [name, d] (name)}
      <symbol id="{uid}-{name}" viewBox="0 0 24 24"><path {d} /></symbol>
    {/each}
  </defs>
</svg>

<div class="h-rte" class:h-rte--invalid={!!error} class:h-rte--readonly={readonly} style:--h-rte-rows={rows}>
  {#if !readonly}
    <!-- svelte-ignore a11y_interactive_supports_focus (roving tabindex is set on the buttons) -->
    <div class="h-rte__bar" role="toolbar" aria-label={t('richtext.toolbar')} aria-orientation="horizontal" bind:this={bar} onkeydown={barKey} onfocusin={barFocus}>
      {#if !broken}
        <button type="button" class="h-rte__btn" aria-label={t('richtext.undo')} title={hint(t('richtext.undo'), `${mod}Z`)} disabled={!s?.undo} onmousedown={(e) => e.preventDefault()} onclick={() => run(undoCmd)}>{@render ico('undo')}</button>
        <button type="button" class="h-rte__btn" aria-label={t('richtext.redo')} title={hint(t('richtext.redo'), `${shift}${mod}Z`)} disabled={!s?.redo} onmousedown={(e) => e.preventDefault()} onclick={() => run(redoCmd)}>{@render ico('redo')}</button>
        {#if styleItems.length > 1}
          <span class="h-rte__sep" role="separator" aria-orientation="vertical"></span>
          <Menu items={styleItems} label={t('richtext.style')} align="start" triggerClass="h-rte__style">
            {#snippet trigger()}{styleLabel}<Icon name="chevron-down" size={14} />{/snippet}
          </Menu>
        {/if}
        {#if m.bold || m.italic || m.code || m.link || m.underline || m.strike || m.superscript || m.subscript}<span class="h-rte__sep" role="separator" aria-orientation="vertical"></span>{/if}
        {#if m.bold}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.bold')} aria-pressed={s?.bold ?? false} title={hint(t('richtext.bold'), `${mod}B`)} disabled={!s?.boldOk} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleMark(m.bold))}>{@render ico('bold')}</button>
        {/if}
        {#if m.italic}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.italic')} aria-pressed={s?.italic ?? false} title={hint(t('richtext.italic'), `${mod}I`)} disabled={!s?.italicOk} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleMark(m.italic))}>{@render ico('italic')}</button>
        {/if}
        {#if m.code}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.code')} aria-pressed={s?.code ?? false} title={hint(t('richtext.code'), `${mod}E`)} disabled={!s?.codeOk} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleMark(m.code))}>{@render ico('code')}</button>
        {/if}
        {#each extraMarks as [k, key] (k)}
          {#if m[k]}
            <button type="button" class="h-rte__btn" aria-label={t(`richtext.${k}`)} aria-pressed={s?.extra[k]?.on ?? false} title={hint(t(`richtext.${k}`), k === 'strike' ? key : `${mod}${key}`)} disabled={!s?.extra[k]?.ok} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleMark(m[k]))}>{@render ico(k)}</button>
          {/if}
        {/each}
        {#if m.link}
          <span class="h-rte__linkwrap" bind:this={linkBtn} role="presentation">
            <Popover label={t('richtext.linkTitle')} triggerClass="h-rte__btn {s?.link ? 'h-rte__btn--on' : ''}" bind:open={linkOpen}>
              {#snippet trigger()}<Icon name="link" size={16} /><span class="h-sr">{t('richtext.link')}</span>{/snippet}
              {#snippet children(close)}
                <LinkForm
                  href={s?.href ?? ''}
                  {locale}
                  onapply={(href, text) => {
                    close();
                    run(linkApi!.set(href, text));
                  }}
                  onremove={() => {
                    close();
                    run(linkApi!.remove);
                  }}
                />
              {/snippet}
            </Popover>
          </span>
        {/if}
        {#if n.bulletList || n.orderedList || n.blockquote || n.codeBlock || n.horizontalRule || n.image || n.table}<span class="h-rte__sep" role="separator" aria-orientation="vertical"></span>{/if}
        {#if n.bulletList}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.bulletList')} aria-pressed={s?.bullet ?? false} title={hint(t('richtext.bulletList'), `${shift}Ctrl+8`)} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleList(schema!, 'bulletList'))}><Icon name="list" size={16} /></button>
        {/if}
        {#if n.orderedList}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.orderedList')} aria-pressed={s?.ordered ?? false} title={hint(t('richtext.orderedList'), `${shift}Ctrl+9`)} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleList(schema!, 'orderedList'))}>{@render ico('list-ordered')}</button>
        {/if}
        {#if n.blockquote}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.blockquote')} aria-pressed={s?.quote ?? false} title={t('richtext.blockquote')} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleQuote(schema!))}>{@render ico('quote')}</button>
        {/if}
        {#if n.codeBlock}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.codeBlock')} aria-pressed={s?.codeBlock ?? false} title={t('richtext.codeBlock')} onmousedown={(e) => e.preventDefault()} onclick={() => run(toggleBlock(schema!, 'codeBlock'))}>{@render ico('code-block')}</button>
        {/if}
        {#if n.horizontalRule}
          <button type="button" class="h-rte__btn" aria-label={t('richtext.horizontalRule')} title={t('richtext.horizontalRule')} onmousedown={(e) => e.preventDefault()} onclick={() => run(insertRule(schema!))}><Icon name="minus" size={16} /></button>
        {/if}
        {#if n.image}
          <Popover label={t('richtext.imageTitle')} triggerClass="h-rte__btn" bind:open={imageOpen}>
            {#snippet trigger()}<Icon name="image" size={16} /><span class="h-sr">{t('richtext.image')}</span>{/snippet}
            {#snippet children(close)}
              <ImageForm
                onapply={(src, alt) => {
                  close();
                  run(insertImage(schema!, { src, alt }));
                }}
              />
            {/snippet}
          </Popover>
        {/if}
        {#if n.table}
          <Menu items={tableItems} label={t('richtext.table')} align="start" triggerClass="h-rte__btn">
            {#snippet trigger()}{@render ico('table')}{/snippet}
          </Menu>
        {/if}
      {/if}
      <span class="h-spacer"></span>
      {#if menu.length}<Menu items={menu} label={t('richtext.more')} />{/if}
    </div>
  {/if}
  {#if broken}
    <p class="h-rte__broken" role="alert"><Icon name="alert-circle" size={14} />{t('richtext.invalid')}</p>
    {#if json}<div class="h-rte__brokenact"><Button size="sm" onclick={openJson}>{t('richtext.editJson')}</Button></div>{/if}
  {/if}
  <div class="h-rte__host" hidden={broken} bind:this={host}></div>
</div>

<Dialog bind:open={jsonOpen} title={t('richtext.jsonTitle', { field: label })} size="lg">
  <Textarea rows={16} class="h-rte__json" bind:value={jsonText} spellcheck={false} invalid={!!jsonProblem} aria-label={t('richtext.jsonTitle', { field: label })} />
  {#if jsonProblem}<p class="h-field__error" role="alert">{jsonProblem}</p>{/if}
  {#snippet footer()}
    <Button onclick={() => (jsonOpen = false)}>{t('common.cancel')}</Button>
    <Button variant="primary" onclick={applyJson}>{t('richtext.jsonApply')}</Button>
  {/snippet}
</Dialog>
