<script lang="ts">
  import Button from '../ui/Button.svelte';
  import { t } from '../i18n';
  import '../messages/media';
  import { assetPath, focalFromPoint, focalPercent, focalStep, validFocal, type FileValue, type Focal } from './logic';

  let {
    file,
    value = $bindable(),
    readonly = false,
    onchange,
  }: {
    file: FileValue;
    value?: Focal | undefined;
    readonly?: boolean;
    onchange?: (f: Focal | undefined) => void;
  } = $props();

  const uid = $props.id();
  let stage = $state<HTMLElement>();
  let dragging = false;
  const focal = $derived(validFocal(value));
  const pct = $derived(focalPercent(focal));
  const text = $derived(t('media.focalValue', { x: pct.x, y: pct.y }));

  function set(f: Focal | undefined) {
    value = f;
    onchange?.(f);
  }
  function at(e: PointerEvent) {
    if (stage) set(focalFromPoint(stage.getBoundingClientRect(), e.clientX, e.clientY));
  }
  function down(e: PointerEvent) {
    if (readonly || e.button !== 0) return;
    dragging = true;
    stage?.setPointerCapture(e.pointerId);
    stage?.focus();
    at(e);
  }
  function move(e: PointerEvent) {
    if (dragging) at(e);
  }
  function up(e: PointerEvent) {
    dragging = false;
    stage?.releasePointerCapture?.(e.pointerId);
  }
  function key(e: KeyboardEvent) {
    if (readonly) return;
    const next = focalStep(focal, e.key, e.shiftKey);
    if (!next) return;
    e.preventDefault();
    set(next);
  }
</script>

<div class="h-focal">
  <!-- A two-axis position has no native control: a slider whose value text names both axes. -->
  <div
    bind:this={stage}
    class="h-focal__stage"
    class:h-focal__stage--ro={readonly}
    role="slider"
    tabindex={readonly ? -1 : 0}
    aria-label={t('media.focal')}
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={pct.x}
    aria-valuetext={focal ? text : t('media.focalUnset')}
    aria-readonly={readonly || undefined}
    aria-describedby="{uid}-hint"
    onpointerdown={down}
    onpointermove={move}
    onpointerup={up}
    onpointercancel={up}
    onkeydown={key}
  >
    <img src={assetPath(file)} alt="" draggable="false" />
    <span class="h-focal__mark" class:h-focal__mark--unset={!focal} style:left="{pct.x}%" style:top="{pct.y}%" aria-hidden="true"></span>
  </div>
  <p class="h-muted h-focal__hint" id="{uid}-hint">{focal ? text : t('media.focalUnset')}. {t('media.focalHint')}</p>
  {#if focal && !readonly}<Button size="sm" variant="ghost" onclick={() => set(undefined)}>{t('media.focalReset')}</Button>{/if}
</div>
