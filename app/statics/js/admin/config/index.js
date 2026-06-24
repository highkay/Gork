import {
  loadClassicScript,
  versionedScript,
} from '../../shared/module-loader.js';
import * as formRender from './form-render.js';
import * as patch from './patch.js';
import * as schema from './schema.js';
import * as status from './status.js';

window.AdminConfigModules = { formRender, patch, schema, status };

await loadClassicScript(
  versionedScript('/static/js/admin/config/legacy.js', import.meta.url),
);
