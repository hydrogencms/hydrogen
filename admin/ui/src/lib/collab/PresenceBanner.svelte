<script lang="ts">
  import '../messages/collab';
  import { t } from '../i18n';

  let { kind = 'unavailable' }: { kind?: 'unavailable' | 'failed' | 'readOnly' } = $props();
</script>

<p class="h-alert" data-testid={kind === 'unavailable' ? 'collab-unavailable' : kind === 'failed' ? 'collab-failed' : 'collab-readonly'}>{t(`collab.${kind}`)}</p>
