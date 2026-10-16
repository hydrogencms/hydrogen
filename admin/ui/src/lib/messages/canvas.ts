import { register } from '../i18n';

// Keys for the canvas over the preview: prefix every key with "canvas."
export const canvas: Record<string, string> = {
  'canvas.toggle': 'Edit on canvas',
  'canvas.unsupported': "This frontend's bridge doesn't support canvas editing. Update @hydrogencms/bridge.",
  'canvas.block': 'Block: {label}',
  'canvas.field': 'Field: {label}',
  'canvas.toolbar': 'Selection tools',
  'canvas.drag': 'Drag to move',
  'canvas.dragHint': 'Use Alt+arrow keys to move',
  'canvas.parent': 'Select parent',
  'canvas.noParent': 'This block is at the top level',
  'canvas.insertAfter': 'Insert after',
  'canvas.addInside': 'Add inside',
  'canvas.slotItem': '{label}',
  'canvas.editText': 'Edit text',
  'canvas.showInForm': 'Show in form',
  'canvas.editShared': 'Edit shared',
  'canvas.detach': 'Detach',
  'canvas.emptyAdd': 'Add block to {label}',
  'canvas.selected': 'Selected {label}',
  'canvas.cleared': 'Selection cleared',
  'canvas.dropping': 'Dragging {label}',
  'canvas.insertIn': 'Add a block to {label}',
};

register(canvas);
