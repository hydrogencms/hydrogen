import { register } from '../i18n';

// Keys for locale inheritance (market overlays) in the item editor: prefix every key with "overlay."
export const overlays: Record<string, string> = {
  'overlay.inheritedFrom': 'Inherited from {locale}',
  'overlay.overridden': 'Overridden',
  'overlay.override': 'Override',
  'overlay.reset': 'Reset to {locale}',
  'overlay.inherits': 'inherits {locale}',
  'overlay.banner': 'Editing {locale}, which inherits {parent}. Only the fields you override are stored here.',
  'overlay.state.derived': 'published through {locale}',
  'overlay.state.derivedPlain': 'published through inheritance',
  'overlay.state.overrides': 'has overrides',
  'overlay.state.draft': 'draft overrides',
};

register(overlays);
