import {
  loadClassicScript,
  versionedScript,
} from '../../shared/module-loader.js';
import * as accountApi from './api.js';
import * as accountBatchActions from './batch-actions.js';
import * as accountFilters from './filters.js';
import * as accountImportDialog from './import-dialog.js';
import * as accountTable from './table.js';

window.AdminAccountModules = {
  api: accountApi,
  batchActions: accountBatchActions,
  filters: accountFilters,
  importDialog: accountImportDialog,
  table: accountTable,
};

await loadClassicScript(
  versionedScript('/static/js/admin/account/legacy.js', import.meta.url),
);
