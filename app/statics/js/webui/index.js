import { loadClassicScript, versionedScript } from '../shared/module-loader.js';
import * as api from './api.js';
import * as attachments from './attachments.js';
import * as render from './render.js';
import * as storage from './storage.js';
import * as streamParser from './stream-parser.js';

window.WebUIChatModules = { api, attachments, render, storage, streamParser };

await loadClassicScript(
  versionedScript('/static/js/webui/legacy-chat.js', import.meta.url),
);
