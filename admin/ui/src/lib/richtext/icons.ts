// Icons only the editor toolbar uses; drawn from a sprite the editor chunk mounts itself, so the initial
// bundle does not carry them. Same grid and style as ui/icons.ts.
export const editorIcons: Record<string, string> = {
  bold: 'M7 4.5h5.5a3.5 3.5 0 0 1 0 7H7zM7 11.5h6.5a3.5 3.5 0 0 1 0 7H7z',
  italic: 'M10 4.5h7M7 19.5h7M14.5 4.5l-5 15',
  code: 'm8.5 8-4 4 4 4M15.5 8l4 4-4 4M13.5 5.5l-3 13',
  'code-block': 'M5 4.5h14a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-13a1 1 0 0 1 1-1zM10 9.5 7.5 12l2.5 2.5M14 9.5l2.5 2.5-2.5 2.5',
  'list-ordered': 'M10 6.5h10M10 12h10M10 17.5h10M4.5 5l1.2-.8v4M4.5 13.5h2.2L4.5 16.5h2.4',
  quote: 'M5 17.5v-5a4 4 0 0 1 4-4M13 17.5v-5a4 4 0 0 1 4-4M5 14h4v3.5H5zM13 14h4v3.5h-4z',
  undo: 'M9 14 4.5 9.5 9 5M4.5 9.5H15a4.5 4.5 0 0 1 0 9h-3',
  redo: 'm15 14 4.5-4.5L15 5M19.5 9.5H9a4.5 4.5 0 0 0 0 9h3',
};
