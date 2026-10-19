// The one place that orders the docs site. Each entry is a docs/<slug>.md page; README.md is the index page.
export const groups = [
  { label: 'Start', pages: ['index', 'getting-started', 'concepts'] },
  { label: 'Guides', pages: ['deployment'] },
  { label: 'Reference', pages: ['schema', 'configuration', 'delivery-api', 'management-api', 'access', 'cli', 'hooks'] },
  {
    label: 'Modules',
    pages: [
      'modules', 'module-versions', 'images', 'search', 'meilisearch', 'graphql', 'wasm-hooks', 'site', 'site-export',
      'site-proxy', 'media', 'locales', 'collaboration', 'releases', 'reviews', 'share-links', 'admin-extensions',
    ],
  },
  { label: 'Frontends', pages: ['adapter-kit', 'bridge-protocol'] },
];
export const otherLabel = 'Other';
