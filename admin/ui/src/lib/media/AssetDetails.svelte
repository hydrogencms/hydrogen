<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError } from '../api/client';
  import { reportError } from '../errors';
  import { knownLocales, itemLocales } from '../fields/locales';
  import { cleanDoc, fieldsPatch, jsonEqual, type Doc } from '../fields/logic';
  import { t } from '../i18n';
  import '../messages/collections';
  import '../messages/media';
  import { href, link } from '../router.svelte';
  import { session } from '../auth/session.svelte';
  import { types, type CompiledField } from '../types.svelte';
  import { itemApi, type ItemData } from '../../routes/item/model';
  import Confirm from '../../routes/settings/Confirm.svelte';
  import Badge from '../ui/Badge.svelte';
  import Button from '../ui/Button.svelte';
  import Field from '../ui/Field.svelte';
  import Icon from '../ui/Icon.svelte';
  import IconButton from '../ui/IconButton.svelte';
  import Input from '../ui/Input.svelte';
  import Skeleton from '../ui/Skeleton.svelte';
  import Time from '../ui/Time.svelte';
  import { toast } from '../ui/toast.svelte';
  import FieldsHost from './FieldsHost.svelte';
  import FilePreview from './FilePreview.svelte';
  import { addTags, assetPath, checkFile, dimensions, fileOf, formatSize, maxUpload, normFolder, type FileValue, type Focal } from './logic';
  import { mediaApi, uploadFile } from './upload';
  import './media.css';

  let {
    id,
    folders = [],
    dirty = $bindable(false),
    onclose,
    onchanged,
    onremoved,
  }: {
    id: string;
    /** Folder paths in use (suggestions). */
    folders?: string[];
    /** Whether there are unsaved changes (the screen asks before leaving them). */
    dirty?: boolean;
    onclose: () => void;
    /** Saved, published or replaced: the lists should re-read. `facets`: the folder or tags changed too. */
    onchanged: (change?: { facets: boolean }) => void;
    onremoved: () => void;
  } = $props();

  const uid = $props.id();
  const BUILTIN = ['file', 'folder', 'tags'];

  let data = $state<ItemData | null>(null);
  let failed = $state(false);
  let locales = $state<string[]>([]);
  let inherits = $state<Record<string, boolean>>({});
  let shared = $state<Doc>({});
  let savedShared = $state.raw<Doc>({});
  let loc = $state<Record<string, Doc>>({});
  let savedLoc = $state.raw<Record<string, Doc>>({});
  let tagText = $state('');
  let errors = $state<Record<string, string>>({});
  let saving = $state(false);
  let replacing = $state(false);
  let replaceError = $state('');
  let trashOpen = $state(false);
  let refs = $state<Awaited<ReturnType<typeof mediaApi.references>> | null>(null);
  let input = $state<HTMLInputElement>();
  let reloadTick = $state(0);

  const ty = $derived(data ? types.get(data.type) : undefined);
  const translatable = $derived((ty?.fields ?? []).filter((f) => f.translatable && !BUILTIN.includes(f.name)));
  const sharedFields = $derived((ty?.fields ?? []).filter((f) => !f.translatable));
  const projectShared = $derived(sharedFields.filter((f) => !BUILTIN.includes(f.name)));
  const main = $derived(data?.mainLocale ?? '');
  const canEdit = $derived((data?.actions?.edit ?? true) && !!ty && !!data && session.can('content.edit', { type: data.type }));
  const file = $derived(fileOf(shared.file));

  const pick = (fields: CompiledField[], doc: Doc): Doc => Object.fromEntries(fields.filter((f) => f.name in doc).map((f) => [f.name, structuredClone(doc[f.name])]));
  const folderOf = (v: unknown) => (typeof v === 'string' && v.trim() && normFolder(v) !== '/' ? normFolder(v) : undefined);
  const normShared = (d: Doc): Doc => ({ ...d, folder: folderOf(d.folder) });

  function sharedPatch(): Doc {
    if (!ty) return {};
    return fieldsPatch(sharedFields, normShared(savedShared), normShared($state.snapshot(shared) as Doc));
  }
  function localePatch(l: string): Doc {
    return fieldsPatch(translatable, savedLoc[l] ?? {}, $state.snapshot(loc[l] ?? {}) as Doc, !!inherits[l]);
  }
  const isDirty = $derived.by(() => {
    if (!data || !ty) return false;
    return Object.keys(sharedPatch()).length > 0 || locales.some((l) => Object.keys(localePatch(l)).length > 0);
  });
  $effect(() => {
    dirty = isDirty;
  });
  $effect(() => () => {
    dirty = false;
  });
  const alt = $derived(typeof loc[main]?.alt === 'string' ? (loc[main]!.alt as string).trim() : '');
  const hasAlt = $derived(translatable.some((f) => f.name === 'alt'));
  const edited = (path: string) => {
    if (path in errors) delete errors[path];
  };

  async function load(itemId: string, signal?: AbortSignal, keepErrors = false) {
    failed = false;
    try {
      const first = await itemApi.read({ kind: 'item', id: itemId }, '', signal);
      const def = types.get(first.type);
      const tr = (def?.fields ?? []).filter((f) => f.translatable && !BUILTIN.includes(f.name));
      const ls = tr.length ? [...new Set([first.locale, ...itemLocales(knownLocales(), first)])] : [first.locale];
      const rest = await Promise.all(ls.slice(1).map((l) => itemApi.read({ kind: 'item', id: itemId }, l, signal)));
      if (signal?.aborted) return;
      const reads = [first, ...rest];
      const s: Doc = {};
      for (const f of def?.fields ?? []) if (!f.translatable && first.fields[f.name] !== undefined) s[f.name] = structuredClone(first.fields[f.name]);
      shared = structuredClone(s);
      savedShared = structuredClone(s);
      const docs: Record<string, Doc> = {};
      const inh: Record<string, boolean> = {};
      reads.forEach((r, i) => {
        docs[ls[i]!] = pick(tr, r.fields);
        inh[ls[i]!] = !!r.inherits;
      });
      loc = structuredClone(docs);
      savedLoc = structuredClone(docs);
      inherits = inh;
      locales = ls;
      data = first;
      if (!keepErrors) errors = {};
    } catch (e) {
      if (signal?.aborted) return;
      failed = true;
      void e;
    }
  }

  $effect(() => {
    const itemId = id;
    void reloadTick;
    const ctl = new AbortController();
    untrack(() => {
      if (data && data.id !== itemId) {
        data = null;
        refs = null;
      }
      tagText = '';
      replaceError = '';
      void load(itemId, ctl.signal);
    });
    return () => ctl.abort();
  });

  $effect(() => {
    const itemId = id;
    void reloadTick;
    const ctl = new AbortController();
    mediaApi.references(itemId, ctl.signal).then((r) => (refs = r)).catch(() => {});
    return () => ctl.abort();
  });

  async function save() {
    if (!data || saving) return;
    const sp = sharedPatch();
    const calls: { locale: string; fields: Doc }[] = [];
    for (const l of locales) {
      const fields = { ...(l === main ? sp : {}), ...localePatch(l) };
      if (Object.keys(fields).length) calls.push({ locale: l, fields });
    }
    if (!calls.length) return;
    saving = true;
    errors = {};
    const facets = 'folder' in sp || 'tags' in sp;
    let saved = 0;
    try {
      for (const c of calls) {
        await itemApi.update({ kind: 'item', id: data.id }, c);
        saved++;
      }
      toast.success(t('media.saved'));
      reloadTick++;
      onchanged({ facets });
    } catch (e) {
      let shown: Record<string, string> = {};
      let invalid = '';
      if (e instanceof ApiError && e.fields?.length) {
        shown = Object.fromEntries(e.fields.map((f) => [f.path, f.message]));
        invalid = e.message;
      } else if (e instanceof ApiError) reportError(e);
      else throw e;
      if (saved > 0) {
        // Some languages are stored and others are not: show what the server holds now, with the error messages.
        await load(data.id, undefined, true);
        toast.error(t('media.partialSave'));
        onchanged({ facets });
      } else if (invalid) toast.error(invalid);
      errors = shown;
    } finally {
      saving = false;
    }
  }

  async function publish() {
    if (!data) return;
    try {
      await itemApi.publish({ kind: 'item', id: data.id }, {});
      toast.success(t('media.published'));
      reloadTick++;
      onchanged();
    } catch (e) {
      reportError(e);
    }
  }

  async function replace(e: Event) {
    const el = e.currentTarget as HTMLInputElement;
    const f = el.files?.[0];
    el.value = '';
    if (!f || !data) return;
    replaceError = '';
    const bad = checkFile(ty?.upload, f, maxUpload());
    if (bad) {
      replaceError = bad === 'size' ? t('media.tooBig', { name: f.name }) : t('media.notAccepted', { name: f.name });
      return;
    }
    replacing = true;
    try {
      const up = await uploadFile(f, { type: data.type });
      shared.file = up.file;
    } catch (err) {
      replaceError = err instanceof Error ? err.message : t('media.failed');
    } finally {
      replacing = false;
    }
  }

  function setFocal(f: Focal | undefined) {
    if (file) shared.file = { ...file, focal: f } as FileValue;
  }

  function tagKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commitTag();
    } else if (e.key === 'Backspace' && !tagText && tags.length) {
      shared.tags = tags.slice(0, -1);
    }
  }
  function commitTag() {
    if (tagText.trim()) {
      shared.tags = addTags(tags, tagText);
      edited('tags');
    }
    tagText = '';
  }
  const tags = $derived(Array.isArray(shared.tags) ? (shared.tags as string[]) : []);

  async function trash() {
    if (!data) return;
    const name = data.name;
    try {
      await itemApi.remove(data.id);
    } catch (e) {
      reportError(e);
      throw e;
    }
    toast.success(t('media.trashed', { name }));
    onremoved();
  }

  const localeName = (l: string) => l;
</script>

<aside class="h-assetpanel" aria-label={t('media.details')}>
  {#if failed}
    <p class="h-alert" role="alert">{t('media.detailsFailed')}</p>
    <Button size="sm" onclick={onclose}>{t('common.close')}</Button>
  {:else if !data || !ty}
    <Skeleton lines={6} height="1.2rem" />
  {:else}
    <header class="h-assetpanel__head">
      <h2 class="h-assetpanel__title">{data.name}</h2>
      <IconButton icon="x" size="sm" label={t('media.closeDetails')} onclick={onclose} />
    </header>

    <div class="h-assetpanel__preview">
      <FilePreview {file} readonly={!canEdit || replacing} onfocal={setFocal} />
    </div>

    {#if file}
      <dl class="h-assetpanel__meta">
        <div><dt>{t('media.file')}</dt><dd class="h-assetpanel__fname">{file.name}</dd></div>
        <div><dt>{t('media.fileType')}</dt><dd>{file.mime}</dd></div>
        <div><dt>{t('media.fileSize')}</dt><dd class="h-num">{formatSize(file.size)}</dd></div>
        {#if dimensions(file)}<div><dt>{t('media.fileDimensions')}</dt><dd class="h-num">{dimensions(file)}</dd></div>{/if}
        {#if hasAlt && file.mime.startsWith('image/')}<div><dt>{t('media.altText')}</dt><dd class:h-muted={!alt}>{alt || t('media.altMissing')}</dd></div>{/if}
        <div><dt>{t('media.uploaded')}</dt><dd><Time at={data.createdAt} /></dd></div>
        <div>
          <dt>{t('media.status')}</dt>
          <dd><Badge tone={data.status === 'published' ? 'success' : data.status === 'changed' ? 'warning' : 'neutral'}>{t(`collections.status.${data.status}`)}</Badge></dd>
        </div>
      </dl>
    {/if}

    <form class="h-assetpanel__form" onsubmit={(e) => { e.preventDefault(); void save(); }}>
      {#if projectShared.length}
        <FieldsHost fields={projectShared} bind:doc={shared} locale={main} type={data.type} {errors} onedit={edited} readonly={!canEdit} />
      {/if}

      {#each locales as l (l)}
        {#if translatable.length}
          {#if locales.length > 1}
            <fieldset class="h-assetpanel__locale">
              <legend>{t('media.inLocale', { locale: localeName(l) })}</legend>
              <FieldsHost fields={translatable} bind:doc={loc[l]} locale={l} type={data.type} {errors} onedit={edited} readonly={!canEdit} />
            </fieldset>
          {:else}
            <FieldsHost fields={translatable} bind:doc={loc[l]} locale={l} type={data.type} {errors} onedit={edited} readonly={!canEdit} />
          {/if}
        {/if}
      {/each}

      <Field label={t('media.folder')} description={t('media.folderHint')} error={errors.folder}>
        {#snippet children(c)}
          <Input {...c} list="{uid}-folders" autocomplete="off" disabled={!canEdit} value={typeof shared.folder === 'string' ? shared.folder : ''} oninput={(e) => { shared.folder = (e.currentTarget as HTMLInputElement).value; edited('folder'); }} />
          <datalist id="{uid}-folders">{#each folders as f (f)}<option value={f}></option>{/each}</datalist>
        {/snippet}
      </Field>

      <Field label={t('media.tags')} description={t('media.tagsHint')} error={errors.tags}>
        {#snippet children(c)}
          <div class="h-tagbox">
            {#each tags as tag (tag)}
              <span class="h-tagbox__tag">{tag}
                {#if canEdit}<button type="button" class="h-tagbox__x" aria-label={t('media.removeTag', { tag })} onclick={() => { shared.tags = tags.filter((x) => x !== tag); edited('tags'); }}><Icon name="x" size={12} /></button>{/if}
              </span>
            {/each}
            <Input {...c} class="h-tagbox__input" autocomplete="off" disabled={!canEdit} bind:value={tagText} onkeydown={tagKey} onblur={commitTag} />
          </div>
        {/snippet}
      </Field>

      {#if replaceError}<p class="h-field__error" role="alert"><Icon name="alert-circle" size={14} />{replaceError}</p>{/if}
      {#if file && shared.file !== undefined && (savedShared.file as FileValue | undefined)?.id !== file.id}
        <p class="h-muted">{t('media.replaceNote')}</p>
      {/if}

      <div class="h-assetpanel__actions">
        {#if canEdit}
          <Button type="submit" variant="primary" loading={saving} disabled={!isDirty}>{t('media.save')}</Button>
          <Button disabled={isDirty || data.status === 'published' || saving} onclick={() => void publish()}>{t('media.publish')}</Button>
          <Button icon="upload" loading={replacing} onclick={() => input?.click()}>{t('media.replace')}</Button>
          <input bind:this={input} hidden type="file" accept={ty.upload?.accept?.join(',')} onchange={replace} />
        {/if}
        {#if file}<a class="h-button h-button--secondary h-button--md" href={assetPath(file)} download={file.name}><Icon name="download" size={16} />{t('media.download')}</a>{/if}
        <a class="h-button h-button--secondary h-button--md" use:link href={href(`/items/${data.id}`)}><Icon name="edit" size={16} />{t('media.openEditor')}</a>
        {#if session.can('content.delete', { type: data.type }) && (data.actions?.delete ?? true)}
          <Button variant="danger" icon="trash" onclick={() => (trashOpen = true)}>{t('media.trash')}</Button>
        {/if}
      </div>
      {#if isDirty}<p class="h-muted" role="status">{t('media.unsaved')}</p>{/if}
    </form>

    <section class="h-assetpanel__usage">
      <h3>{t('media.usedBy')}</h3>
      {#if refs === null}
        <Skeleton lines={2} />
      {:else if refs.items.length === 0 && !refs.hidden}
        <p class="h-muted">{t('media.unused')}</p>
      {:else}
        <ul aria-label={t('media.usedBy')} class="h-assetpanel__refs">
          {#each refs.items as r (r.id + r.field)}
            <li><a use:link href={href(`/items/${r.id}`)}>{r.name || r.id}</a> <span class="h-muted">{types.get(r.type)?.label ?? r.type} · {t('media.usedField', { field: r.field })}</span></li>
          {/each}
        </ul>
        {#if refs.hidden}<p class="h-muted">{t('media.usedHidden', { count: refs.hidden })}</p>{/if}
      {/if}
    </section>
  {/if}
</aside>

<Confirm bind:open={trashOpen} title={t('media.trashTitle')} message={t('media.trashText', { name: data?.name ?? '' })} confirm={t('media.trash')} onconfirm={trash} />
