import {
  loadClassicScript,
  versionedScript,
} from '../../shared/module-loader.js';
import * as api from './api.js';
import * as list from './list.js';
import * as pagination from './pagination.js';
import * as stats from './stats.js';

window.AdminCacheModules = { api, list, pagination, stats };

await loadClassicScript(
  versionedScript('/static/js/admin/cache/legacy.js', import.meta.url),
);
