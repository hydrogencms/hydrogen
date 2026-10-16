<script lang="ts">
  import '../messages/collab';
  import './Presence.css';
  import { t } from '../i18n';
  import { applyMarks } from './marks';
  import type { Participant } from './transport';

  /** Draws the other participants' focus on the form's fields and block cards (no markup of its own). */
  let { root, others }: { root: HTMLElement | undefined; others: Participant[] } = $props();

  $effect(() => {
    const el = root;
    const list = others;
    if (!el) return;
    let raf = 0;
    const mo = new MutationObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(run);
    });
    // Drawing changes the DOM itself (description text): the observer must not see its own work.
    function run() {
      mo.disconnect();
      applyMarks(el!, list, (name) => t('collab.editing', { name }), (n) => t('collab.editingMany', { n }));
      mo.observe(el!, { childList: true, subtree: true });
    }
    run();
    return () => {
      mo.disconnect();
      cancelAnimationFrame(raf);
      applyMarks(el, [], () => '', () => '');
    };
  });
</script>
