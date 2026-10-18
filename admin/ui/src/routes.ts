import type { Route } from './lib/router.svelte';
import Login from './routes/login/Login.svelte';

const publishing = () => import('./routes/publishing/Route.svelte');

// First match wins. Routes of the content, pages and settings screens load lazily (one chunk each).
export const routes: Route[] = [
  { path: '/login', public: true, title: 'login.title', load: async () => ({ default: Login }) },
  { path: '/', title: 'dashboard.title', load: () => import('./routes/dashboard/Dashboard.svelte') },
  { path: '/search', title: 'search.title', load: () => import('./routes/search/Search.svelte') },

  { path: '/collections/:type', title: 'nav.content', load: () => import('./routes/collection/List.svelte') },
  { path: '/collections/:type/new', title: 'nav.content', load: () => import('./routes/collection/Create.svelte') },
  { path: '/items/:id/compare', title: 'nav.content', load: () => import('./routes/item/Compare.svelte') },
  { path: '/items/:id', title: 'nav.content', load: () => import('./routes/item/Item.svelte') },
  { path: '/singletons/:type', title: 'nav.settingsContent', load: () => import('./routes/singleton/Singleton.svelte') },

  { path: '/assets', title: 'nav.assets', load: () => import('./routes/assets/Assets.svelte') },

  { path: '/pages/:site', title: 'nav.pages', load: () => import('./routes/pages/Pages.svelte') },

  // One lazy entry for both: the list at `/x`, the detail at `/x/:id` (params.rest).
  { path: '/releases/*', title: 'nav.releases', load: publishing },
  { path: '/reviews', title: 'nav.reviews', load: () => import('./routes/review/Reviews.svelte') },
  { path: '/trash/*', title: 'nav.trash', load: publishing },

  { path: '/settings/users', title: 'nav.users', load: () => import('./routes/settings/Users.svelte') },
  { path: '/settings/users/:id', title: 'nav.users', load: () => import('./routes/settings/UserEdit.svelte') },
  { path: '/settings/groups', title: 'nav.groups', load: () => import('./routes/settings/Groups.svelte') },
  { path: '/settings/groups/:id', title: 'nav.groups', load: () => import('./routes/settings/GroupEdit.svelte') },
  { path: '/settings/roles', title: 'nav.roles', load: () => import('./routes/settings/Roles.svelte') },
  { path: '/settings/roles/:id', title: 'nav.roles', load: () => import('./routes/settings/RoleEdit.svelte') },
  { path: '/settings/tokens', title: 'nav.tokens', load: () => import('./routes/settings/Tokens.svelte') },
  { path: '/settings/system', title: 'nav.system', load: () => import('./routes/settings/System.svelte') },
  { path: '/settings/audit', title: 'nav.audit', load: () => import('./routes/settings/Audit.svelte') },

  { path: '/account', title: 'account.title', load: () => import('./routes/account/Account.svelte') },
];

export const notFound = () => import('./routes/NotFound.svelte');
