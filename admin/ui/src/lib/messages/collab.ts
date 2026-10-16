import { register } from '../i18n';

// Keys for presence and soft locks in the item editor: prefix every key with "collab."
export const collab: Record<string, string> = {
  'collab.alsoHere': 'Also here',
  'collab.idle': 'idle',
  'collab.viewing': 'viewing',
  'collab.more': '{n} more',
  'collab.editing': '{name} is editing',
  'collab.editingMany': '{n} people are editing',
  'collab.unavailable': 'Collaboration unavailable — you may overwrite others’ changes.',
  'collab.reset.external': 'The draft was changed elsewhere. Reloaded the latest version.',
  'collab.reset.expired': 'The editing session ended. Reloaded the latest version.',
  'collab.reset.trashed': 'This item was moved to the trash.',
  'collab.resetConfirm': 'The draft was changed elsewhere. Reload it and lose your unsynced edits?',
  'collab.failed': 'Your changes could not be shared with the other editors. Save to keep them.',
  'collab.readOnly': 'You can only watch this session: your changes are not shared.',
  'collab.loading': 'Loading the shared content…',
  'collab.joined': '{name} joined',
  'collab.left': '{name} left',
  'collab.editingField': '{name} is editing {field}',
  'collab.saved': '{name} saved this item.',
};

register(collab);
