// ── State ──────────────────────────────────────────────────────────────────
const PAGE_SIZE_KEY = 'admin.account.page_size';
const PAGE_SIZE_OPTIONS = [50, 100, 200, 500, 1000, 2000];

let allTokens = [], curStatus = 'all', curNsfw = 'all', curPool = 'all', curPage = 1, pageSize = loadSavedPageSize();
let pageMeta = { total: 0, page: 1, page_size: pageSize, total_pages: 1 };
let serverFacets = null;
const sel = new Set();
const refreshingTokens = new Set();
let _cb = null;
let _editingToken = '';
let _filterMenuBound = false;
let _tokenViewVersion = 0;
let _tokenViewCacheKey = '';
let _tokenViewCache = null;

function invalidateTokenView() {
  _tokenViewVersion += 1;
  _tokenViewCacheKey = '';
  _tokenViewCache = null;
}

function tr(key, params, fallback) {
  const value = t(key, params);
  return value === key ? (fallback ?? key) : value;
}

function applyAccountI18n() {
  document.title = tr('account.pageTitle', null, 'Gork - 账户管理');
  document.querySelectorAll('#page-size-sel option').forEach(opt => {
    opt.textContent = tr('account.pageSizeOption', { n: opt.value }, `${opt.value} / 页`);
  });
  const toolbarTips = [
    ['btn-filter', 'account.filter', '筛选'],
    ['btn-export', 'account.export', '导出数据'],
    ['btn-nsfw-all', 'account.batchNsfwAll', '全部开启 NSFW'],
    ['btn-refresh-all', 'account.refreshAllManageable', '重新测试全部正常账户'],
    ['btn-delete-invalid-all', 'account.deleteInvalidAll', '删除全部异常'],
    ['btn-nsfw', 'account.batchNsfw', '开启 NSFW'],
    ['btn-refresh', 'account.batchRefresh', '刷新选中'],
    ['btn-disable', 'account.batchDisable', '禁用选中'],
    ['btn-restore', 'account.batchRestore', '恢复选中'],
    ['btn-delete', 'account.batchDelete', '删除选中'],
    ['btn-batch-cancel', 'account.cancel', '取消'],
  ];
  toolbarTips.forEach(([id, key, fallback]) => {
    const el = document.getElementById(id);
    if (!el) return;
    const label = tr(key, null, fallback);
    el.setAttribute('data-tip', label);
    el.setAttribute('aria-label', label);
    el.setAttribute('title', label);
  });
  const pageSizeSel = document.getElementById('page-size-sel');
  if (pageSizeSel) pageSizeSel.value = String(pageSize);
  updateImportFileState();
  render();
}

function waitI18n() {
  return new Promise((resolve) => I18n.onReady(resolve));
}

// ── Boot ───────────────────────────────────────────────────────────────────
(async () => {
  await waitI18n();
  applyAccountI18n();
  await renderAdminHeader?.();
  await renderSiteFooter?.();
  initFilterMenu();
  const key = await adminKey.get();
  if (!key || !await verifyKey(ADMIN_API + '/verify', key).catch(() => false))
    return location.href = '/admin/login';
  load();
})();

// ── API ────────────────────────────────────────────────────────────────────
async function _api(method, path, body) {
  const key = await adminKey.get();
  const isForm = body instanceof FormData;
  const r = await fetch(ADMIN_API + path, {
    method,
    headers: { ...(body != null && !isForm && { 'Content-Type': 'application/json' }), Authorization: `Bearer ${key}` },
    ...(body != null && { body: isForm ? body : JSON.stringify(body) }),
  });
  if (!r.ok) { const d = await r.json().catch(() => ({})); throw new Error(d.detail || r.status); }
  return r.json();
}

// Selection strategy reported by /status — "quota" | "random".
// Controls whether quota columns / stats are rendered (hidden in random mode).
let selectionStrategy = 'quota';

// ── Load ───────────────────────────────────────────────────────────────────
function buildTokenQuery({ page = curPage, size = pageSize } = {}) {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(size),
    sort_by: 'updated_at',
    sort_desc: 'true',
  });
  if (curPool !== 'all') params.set('pool', curPool);
  if (curStatus !== 'all') params.set('status', curStatus);
  if (curNsfw !== 'all') params.set('nsfw', curNsfw);
  return params;
}

async function load() {
  try {
    const params = buildTokenQuery();
    const [data, status] = await Promise.all([
      _api('GET', `/tokens?${params}`),
      _api('GET', '/status').catch(() => null),
    ]);
    if (status && typeof status.selection_strategy === 'string') {
      selectionStrategy = status.selection_strategy;
    }
    allTokens = Array.isArray(data.tokens)
      ? data.tokens
      : Object.entries(data.tokens || {}).flatMap(([pool, items]) =>
          Array.isArray(items) ? items.map(t => ({ ...t, pool: t.pool || pool })) : []);
    pageMeta = data.pagination || { total: allTokens.length, page: curPage, page_size: pageSize, total_pages: 1 };
    curPage = pageMeta.page || curPage;
    serverFacets = data.facets || null;
    if (pageMeta.total > 0 && curPage > pageMeta.total_pages) {
      curPage = pageMeta.total_pages;
      return load();
    }
    applyStrategyUI();
    invalidateTokenView();
    render();
  } catch (e) { showToast(`${tr('account.loadFailed', null, '加载失败')}: ${e.message}`, 'error'); }
}

function applyStrategyUI() {
  // Random strategy: hide quota-related stat cells and the table column.
  const isRandom = selectionStrategy === 'random';
  const statsGrid = document.getElementById('overview-stats');
  if (statsGrid) statsGrid.classList.toggle('mode-random', isRandom);
  const quotaGrid = document.getElementById('overview-quota-stats');
  if (quotaGrid) quotaGrid.style.display = isRandom ? 'none' : '';
  document.querySelectorAll('[data-quota-only]').forEach(el => {
    el.style.display = isRandom ? 'none' : '';
  });
}

// ── Render ─────────────────────────────────────────────────────────────────
function render() {
  const view = getTokenView();
  renderStats(view);
  renderFilters(view);
  renderTable(view);
  applyStrategyUI();
}

function getTokenView() {
  const cacheKey = `${_tokenViewVersion}|${curStatus}|${curNsfw}|${curPool}`;
  if (_tokenViewCache && _tokenViewCacheKey === cacheKey) return _tokenViewCache;

  const fallbackStats = {
    active: 0, cooling: 0, invalid: 0, disabled: 0,
    calls: 0, success: 0, fail: 0, qa: 0, qf: 0, qe: 0, qh: 0, qb: 0, qc: 0,
  };
  const fallbackStatusCounts = { all: 0, active: 0, cooling: 0, invalid: 0, disabled: 0 };
  const fallbackNsfwCounts = { all: 0, enabled: 0, disabled: 0 };
  const fallbackPoolCounts = new Map([['all', 0]]);
  const poolsSet = new Set(['basic', 'super', 'heavy']);

  for (const token of allTokens) {
    const pool = token.pool || 'basic';
    const quota = token.quota || {};
    const nsfwEnabled = (token.tags || []).includes('nsfw');
    const invalid = isInvalidStatus(token.status);
    const disabled = isDisabledStatus(token.status);

    poolsSet.add(pool);
    const successCount = token.use_count || 0;
    const failCount = token.fail_count || 0;
    fallbackStats.success += successCount;
    fallbackStats.fail += failCount;
    fallbackStats.calls += successCount + failCount;
    fallbackStats.qa += quota.auto?.remaining || 0;
    fallbackStats.qf += quota.fast?.remaining || 0;
    fallbackStats.qe += quota.expert?.remaining || 0;
    fallbackStats.qh += quota.heavy?.remaining || 0;
    fallbackStats.qb += quota.grok_4_3?.remaining || 0;
    fallbackStats.qc += quota.console?.remaining || 0;

    fallbackStatusCounts.all += 1;
    fallbackNsfwCounts.all += 1;
    fallbackPoolCounts.set('all', fallbackPoolCounts.get('all') + 1);
    fallbackPoolCounts.set(pool, (fallbackPoolCounts.get(pool) || 0) + 1);
    if (token.status === 'active') { fallbackStats.active += 1; fallbackStatusCounts.active += 1; }
    if (token.status === 'cooling') { fallbackStats.cooling += 1; fallbackStatusCounts.cooling += 1; }
    if (invalid) { fallbackStats.invalid += 1; fallbackStatusCounts.invalid += 1; }
    if (disabled) { fallbackStats.disabled += 1; fallbackStatusCounts.disabled += 1; }
    if (nsfwEnabled) fallbackNsfwCounts.enabled += 1;
    else fallbackNsfwCounts.disabled += 1;
  }

  if (curPool !== 'all' && !poolsSet.has(curPool)) {
    curPool = 'all';
    return getTokenView();
  }

  const pools = serverFacets?.pools || Object.fromEntries(fallbackPoolCounts);
  Object.keys(pools).forEach(pool => { if (pool !== 'all') poolsSet.add(pool); });
  _tokenViewCacheKey = `${_tokenViewVersion}|${curStatus}|${curNsfw}|${curPool}`;
  _tokenViewCache = {
    stats: serverFacets?.stats || fallbackStats,
    statusCounts: serverFacets?.status || fallbackStatusCounts,
    nsfwCounts: serverFacets?.nsfw || fallbackNsfwCounts,
    poolCounts: new Map(Object.entries(pools)),
    pools: Array.from(poolsSet),
    filteredItems: allTokens,
  };
  return _tokenViewCache;
}

function poolLabel(pool) {
  return tr(`account.pool.${pool}`, null, pool);
}

function isDisabledStatus(status) {
  return status === 'disabled';
}

function isManageableStatus(status) {
  return status === 'active' || status === 'cooling';
}

function isInvalidStatus(status) {
  return !['active', 'cooling', 'disabled'].includes(status);
}

function renderStats(view = getTokenView()) {
  const { stats } = view;
  $('s-total',   serverFacets?.status?.all ?? (pageMeta.total || stats.active + stats.cooling + stats.invalid + stats.disabled));
  $('s-active',  stats.active);
  $('s-cooling', stats.cooling);
  $('s-invalid', stats.invalid);
  $('s-disabled', stats.disabled);
  $('s-calls', fmt(stats.calls));
  $('s-success', fmt(stats.success));
  $('s-rate', fmtRate(stats.success, stats.fail));
  $('s-qa', fmt(stats.qa)); $('s-qf', fmt(stats.qf)); $('s-qe', fmt(stats.qe)); $('s-qh', fmt(stats.qh)); $('s-qb', fmt(stats.qb)); $('s-qc', fmt(stats.qc));
}

function renderFilters(view = getTokenView()) {
  renderStatusFilters(view);
  renderNsfwFilters(view);
  renderPoolFilters(view);
  syncFilterTrigger();
}

function initFilterMenu() {
  if (_filterMenuBound) return;
  const menu = document.getElementById('filter-menu');
  const trigger = document.getElementById('btn-filter');
  const panel = document.getElementById('filter-panel');
  if (!menu || !trigger || !panel) return;
  _filterMenuBound = true;

  const close = () => {
    menu.classList.remove('open');
    trigger.setAttribute('aria-expanded', 'false');
    syncFilterTrigger();
  };

  trigger.addEventListener('click', (event) => {
    event.stopPropagation();
    const open = !menu.classList.contains('open');
    menu.classList.toggle('open', open);
    trigger.setAttribute('aria-expanded', open ? 'true' : 'false');
    syncFilterTrigger();
  });

  panel.addEventListener('click', (event) => event.stopPropagation());
  document.addEventListener('click', (event) => {
    const target = event.target;
    if (!(target instanceof Node) || !menu.contains(target)) close();
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') close();
  });
}

function syncFilterTrigger() {
  const menu = document.getElementById('filter-menu');
  const trigger = document.getElementById('btn-filter');
  if (!menu || !trigger) return;
  const active = curStatus !== 'all' || curPool !== 'all' || curNsfw !== 'all' || menu.classList.contains('open');
  trigger.classList.toggle('is-active', active);
}

function renderStatusFilters(view = getTokenView()) {
  setFilterCount('fc-status-all', view.statusCounts.all);
  setFilterCount('fc-status-active', view.statusCounts.active);
  setFilterCount('fc-status-cooling', view.statusCounts.cooling);
  setFilterCount('fc-status-invalid', view.statusCounts.invalid);
  setFilterCount('fc-status-disabled', view.statusCounts.disabled);
  document.querySelectorAll('[data-status]').forEach(el => el.classList.toggle('active', el.dataset.status === curStatus));
}

function renderNsfwFilters(view = getTokenView()) {
  setFilterCount('fc-nsfw-all', view.nsfwCounts.all);
  setFilterCount('fc-nsfw-enabled', view.nsfwCounts.enabled);
  setFilterCount('fc-nsfw-disabled', view.nsfwCounts.disabled);
  document.querySelectorAll('[data-nsfw]').forEach(el => el.classList.toggle('active', el.dataset.nsfw === curNsfw));
}

function renderPoolFilters(view = getTokenView()) {
  const wrap = document.getElementById('pool-filter-chips');
  if (!wrap) return;
  const pools = view.pools;
  wrap.innerHTML = [
    `<button type="button" class="filter-chip${curPool === 'all' ? ' active' : ''}" data-pool="all" onclick="switchPool('all')"><span>${tr('account.filterTypeAll', null, '全部')}</span><span class="filter-chip-count">${view.poolCounts.get('all') || 0}</span></button>`,
    ...pools.map(pool => {
      const count = view.poolCounts.get(pool) || 0;
      return `<button type="button" class="filter-chip${curPool === pool ? ' active' : ''}" data-pool="${xe(pool)}" onclick="switchPool('${xe(pool)}')"><span>${xe(poolLabel(pool))}</span><span class="filter-chip-count">${count}</span></button>`;
    }),
  ].join('');
}

function setFilterCount(id, value) {
  const el = document.getElementById(id);
  if (el) el.textContent = value;
}

function applyPoolFilter(items, pool = curPool) {
  if (pool === 'all') return items;
  return items.filter(t => (t.pool || 'basic') === pool);
}

function applyStatusFilter(items, status = curStatus) {
  if (status === 'all') return items;
  if (status === 'invalid') return items.filter(t => isInvalidStatus(t.status));
  if (status === 'disabled') return items.filter(t => isDisabledStatus(t.status));
  return items.filter(t => t.status === status);
}

function applyNsfwFilter(items, mode = curNsfw) {
  if (mode === 'all') return items;
  if (mode === 'enabled') return items.filter(t => (t.tags || []).includes('nsfw'));
  return items.filter(t => !(t.tags || []).includes('nsfw'));
}

function filtered() {
  return getTokenView().filteredItems;
}

function preserveScroll(fn) {
  const y = window.scrollY;
  fn();
  requestAnimationFrame(() => window.scrollTo({ top: y, behavior: 'auto' }));
}

function renderTable(view = getTokenView()) {
  const items = view.filteredItems;
  const total = pageMeta.total ?? items.length;
  const pages = Math.max(1, pageMeta.total_pages || Math.ceil(total / pageSize));
  const slice = items;

  document.getElementById('tbody').innerHTML = slice.length
    ? slice.map(rowHtml).join('')
    : `<tr><td colspan="10" class="empty-state">${tr('account.empty', null, '暂无 Token，请点击右上角导入或添加。')}</td></tr>`;

  $('pagi-page', tr('account.pageIndicator', { current: total ? curPage : 0, total: pages }, `第 ${total ? curPage : 0} / ${pages} 页`));
  $('section-table-count', total);
  const prev = document.getElementById('btn-prev'), next = document.getElementById('btn-next');
  prev.disabled = curPage <= 1; next.disabled = curPage >= pages;
  updateBatchBtns();
}

function rowHtml(t) {
  const q      = t.quota || {};
  const isNsfw = (t.tags || []).includes('nsfw');
  const isDisabled = t.status === 'disabled';
  const canManageNsfw = isManageableStatus(t.status);
  const isRefreshing = refreshingTokens.has(t.token);
  const displayToken = tokenDisplay(t);
  const statusText = statusTip(t);
  const success = t.use_count || 0;
  const fail = t.fail_count || 0;
  const nsfwBtn = isNsfw
    ? `<button type="button" class="row-nsfw-btn is-on" ${canManageNsfw ? `data-nsfw-token="${xe(t.token)}" data-nsfw-enabled="false"` : 'disabled'} data-tip="${xe(canManageNsfw ? tr('account.batchNsfwDisable', null, '关闭 NSFW') : tr('account.rowActionNotSupported', null, '当前状态不支持此操作'))}" aria-label="${xe(canManageNsfw ? tr('account.batchNsfwDisable', null, '关闭 NSFW') : tr('account.rowActionNotSupported', null, '当前状态不支持此操作'))}">
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <circle cx="10" cy="10" r="7.25"/>
          <text x="10" y="10.4">18</text>
        </svg>
      </button>`
    : `<button type="button" class="row-nsfw-btn" ${canManageNsfw ? `data-nsfw-token="${xe(t.token)}" data-nsfw-enabled="true"` : 'disabled'} data-tip="${xe(canManageNsfw ? tr('account.batchNsfw', null, '开启 NSFW') : tr('account.rowActionNotSupported', null, '当前状态不支持此操作'))}" aria-label="${xe(canManageNsfw ? tr('account.batchNsfw', null, '开启 NSFW') : tr('account.rowActionNotSupported', null, '当前状态不支持此操作'))}">
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <circle cx="10" cy="10" r="7.25"/>
          <text x="10" y="10.4">18</text>
          <path d="M14.8 5.2 5.2 14.8"/>
        </svg>
      </button>`;
  return `<tr>
    <td><input type="checkbox" class="cb row-cb" data-token="${xe(t.token)}" ${sel.has(t.token)?'checked':''} onchange="toggleRow(this)"></td>
    <td><div class="token-stack">
        <span class="tok">${xe(displayToken)}</span>
        <button onclick="copy('${xe(t.token)}')" class="row-icon-btn" data-tip="${xe(tr('account.actionCopy', null, '复制'))}" aria-label="${xe(tr('account.actionCopy', null, '复制'))}">
          <svg viewBox="0 0 24 24" stroke-width="1.8"><rect x="9" y="9" width="10" height="10" rx="2"/><path d="M7 15H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h7a2 2 0 0 1 2 2v1"/></svg>
        </button>
      </div></td>
    <td class="table-center"><span class="badge badge-${t.pool||'basic'}">${xe(poolLabel(t.pool || 'basic'))}</span></td>
    <td class="table-center">${statusBadge(t.status, statusText)}</td>
    <td data-quota-only><div class="quota-cell">${qpills(q)}</div></td>
    <td class="table-center" style="font-variant-numeric:tabular-nums;color:#8f8f8f">${success}</td>
    <td class="table-center" style="font-variant-numeric:tabular-nums;color:#9a9a9a">${fail}</td>
    <td class="table-center" style="font-variant-numeric:tabular-nums;color:#9a9a9a">${fmtRate(success, fail)}</td>
    <td style="font-size:12px;color:#9a9a9a">${fmtDate(t.last_used_at)}</td>
    <td><div class="row-actions">
      ${nsfwBtn}
      <button onclick="${isDisabled ? `restoreOne('${xe(t.token)}')` : `disableOne('${xe(t.token)}')`}" class="row-icon-btn row-icon-btn-state" data-tip="${xe(isDisabled ? tr('account.actionRestore', null, '恢复账号') : tr('account.actionDisable', null, '禁用账号'))}" aria-label="${xe(isDisabled ? tr('account.actionRestore', null, '恢复账号') : tr('account.actionDisable', null, '禁用账号'))}">
        ${isDisabled
          ? '<svg viewBox="0 0 24 24" stroke-width="1.8"><path d="M3 12a9 9 0 1 0 3-6.708"/><path d="M3 4v5h5"/></svg>'
          : '<svg viewBox="0 0 24 24" stroke-width="1.8"><circle cx="12" cy="12" r="8"/><path d="M8.5 8.5 15.5 15.5"/></svg>'}
      </button>
      <button onclick="refreshOne('${xe(t.token)}')" class="row-icon-btn${isRefreshing ? ' is-loading' : ''}" data-tip="${xe(isRefreshing ? tr('account.refreshing', { n: 1 }, '正在刷新 1 个账户…') : tr('account.actionRefresh', null, '刷新 Usage'))}" aria-label="${xe(tr('account.actionRefresh', null, '刷新 Usage'))}">
        <svg viewBox="0 0 24 24" stroke-width="1.8"><path d="M20 11a8 8 0 0 0-14.6-4.6"/><path d="M4 4v5h5"/><path d="M4 13a8 8 0 0 0 14.6 4.6"/><path d="M20 20v-5h-5"/></svg>
      </button>
      <button onclick="openEdit('${xe(t.token)}')" class="row-icon-btn" data-tip="${xe(tr('account.actionEdit', null, '编辑'))}" aria-label="${xe(tr('account.actionEdit', null, '编辑'))}">
        <svg viewBox="0 0 24 24" stroke-width="1.8"><path d="m4 20 4.5-1 9.5-9.5-3.5-3.5L5 15.5z"/><path d="m13.5 5.5 3.5 3.5"/></svg>
      </button>
      <button onclick="doDelete(['${xe(t.token)}'])" class="row-icon-btn row-icon-danger" data-tip="${xe(tr('account.actionDelete', null, '删除'))}" aria-label="${xe(tr('account.actionDelete', null, '删除'))}">
        <svg viewBox="0 0 24 24" stroke-width="1.8"><path d="M5 7h14"/><path d="M9 7V4h6v3"/><path d="M8 10v7"/><path d="M12 10v7"/><path d="M16 10v7"/><path d="M7 7l1 13h8l1-13"/></svg>
        </button>
    </div></td>
  </tr>`;
}

function qpills(q) {
  const modes = [
    ['auto',     'A', 'q-a', true],
    ['fast',     'F', 'q-f', true],
    ['expert',   'E', 'q-e', true],
    ['heavy',    'H', 'q-h', true],
    ['grok_4_3', 'B', 'q-b', false],
    ['console',  'C', 'q-c', false],
  ];
  return modes.map(([m,l,c,showEmpty]) => {
    const win = q[m];
    if (win == null) {
      if (!showEmpty) return '';
      return `<span class="q-pill q-z" data-tip="${xe(tr(`account.mode.${m}`, null, m))}: 0">${l}:0</span>`;
    }
    const r = win.remaining ?? null;
    return `<span class="q-pill ${r ? c : 'q-z'}" data-tip="${xe(`${tr(`account.mode.${m}`, null, m)}: ${r ?? tr('account.unknown', null, '未知')}/${win.total ?? '?'}`)}">${l}:${r ?? '?'}</span>`;
  }).join('');
}

// ── Nav & Pagination ────────────────────────────────────────────────────────
function switchStatus(status) {
  preserveScroll(() => {
    curStatus = status || 'all';
    curPage = 1;
    sel.clear();
    renderFilters();
    load();
  });
}
function switchNsfw(mode) {
  preserveScroll(() => {
    curNsfw = mode || 'all';
    curPage = 1;
    sel.clear();
    renderFilters();
    load();
  });
}
function switchPool(pool) {
  preserveScroll(() => {
    curPool = pool || 'all';
    curPage = 1;
    sel.clear();
    renderFilters();
    load();
  });
}
function prevPage() { if (curPage > 1) { curPage--; load(); } }
function nextPage() {
  const pages = Math.max(1, pageMeta.total_pages || 1);
  if (curPage < pages) { curPage++; load(); }
}
function changePageSize(v) {
  const next = Number(v);
  pageSize = PAGE_SIZE_OPTIONS.includes(next) ? next : 50;
  localStorage.setItem(PAGE_SIZE_KEY, String(pageSize));
  curPage = 1;
  load();
}

function toggleAll(checked) {
  getTokenView().filteredItems.forEach(t => checked ? sel.add(t.token) : sel.delete(t.token));
  document.querySelectorAll('.row-cb').forEach(el => el.checked = checked);
  updateBatchBtns();
}
function toggleRow(el) {
  el.checked ? sel.add(el.dataset.token) : sel.delete(el.dataset.token);
  const page = getTokenView().filteredItems;
  const cbAll = document.getElementById('cb-all');
  cbAll.checked = page.every(t => sel.has(t.token));
  cbAll.indeterminate = !cbAll.checked && sel.size > 0;
  updateBatchBtns();
}
function updateBatchBtns() {
  const show = sel.size > 0;
  ['btn-nsfw','btn-refresh','btn-disable','btn-restore','btn-delete'].forEach(id =>
    document.getElementById(id).style.display = show ? '' : 'none');
  ['btn-export','btn-nsfw-all','btn-refresh-all','btn-delete-invalid-all','page-size-sel'].forEach(id => {
    const el = document.getElementById(id);
    if (el) el.style.display = show ? 'none' : '';
  });
}

// ── Helpers ────────────────────────────────────────────────────────────────
function $(id, v) { const el = document.getElementById(id); if (el) el.textContent = v; }
function fmt(n) { return n >= 10000 ? (n/1000).toFixed(1)+'k' : n; }
function loadSavedPageSize() {
  const raw = Number(localStorage.getItem(PAGE_SIZE_KEY) || 0);
  return PAGE_SIZE_OPTIONS.includes(raw) ? raw : 50;
}
function fmtRate(success, fail) {
  const total = success + fail;
  if (!total) return tr('account.na', null, '—');
  return `${Math.round(success / total * 100)}%`;
}
function mask(t) { return t.length > 20 ? t.slice(0,8) + '…' + t.slice(-8) : t; }
function xe(s) {
  const helper = window.AdminAccountModules?.table?.escapeAdminCellValue;
  return helper ? helper(s) : escapeAdminCellValue(s);
}
function escapeAdminCellValue(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\\/g, '\\\\')
    .replace(/'/g, "\\'")
    .replace(/"/g, '&quot;');
}
function fmtDate(d) {
  if (!d) return tr('account.na', null, '—');
  const dt = new Date(typeof d === 'number' ? d : d);
  return isNaN(dt)
    ? tr('account.na', null, '—')
    : dt.toLocaleDateString(I18n.getLang() === 'en' ? 'en-US' : 'zh-CN', {month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'});
}
function tokenDisplay(t) {
  return t.token_masked || mask(t.token || '');
}
function statusTip(t) {
  const parts = [];
  if (t.cooldown_reason) parts.push(`${tr('account.cooldownReason', null, '冷却原因')}: ${t.cooldown_reason}`);
  if (t.cooldown_until) parts.push(`${tr('account.cooldownUntil', null, '冷却到期')}: ${fmtDate(t.cooldown_until)}`);
  if (t.last_fail_reason && t.last_fail_reason !== t.cooldown_reason) parts.push(`${tr('account.lastFailReason', null, '失败原因')}: ${t.last_fail_reason}`);
  if (t.state_reason && t.state_reason !== t.cooldown_reason && t.state_reason !== t.last_fail_reason) parts.push(`${tr('account.stateReason', null, '状态原因')}: ${t.state_reason}`);
  return parts.join(' / ');
}
function statusBadge(s, tip = '') {
  const m = {
    active:['badge-active', tr('account.status.active', null, '正常')],
    cooling:['badge-cooling', tr('account.status.cooling', null, '限流')],
    expired:['badge-expired', tr('account.status.expired', null, '过期')],
    disabled:['badge-disabled', tr('account.status.disabled', null, '禁用')],
    invalid:['badge-invalid', tr('account.status.invalid', null, '异常')],
  };
  const [cls, label] = m[s] || ['badge-invalid', s || tr('account.unknown', null, '未知')];
  return `<span class="badge ${cls}"${tip ? ` data-tip="${xe(tip)}"` : ''}>${label}</span>`;
}
async function copy(t) {
  try { await navigator.clipboard.writeText(t); showToast(tr('account.copied', null, '已复制'), 'success'); }
  catch { showToast(tr('account.copyFailed', null, '复制失败'), 'error'); }
}

// ── Add / Import ───────────────────────────────────────────────────────────

// For import modals: includes "auto（自动检测）" as the first / default option.
function fillImportPoolOptions(id) {
  const known = [...new Set(['basic', 'super', 'heavy', ...allTokens.map(t => t.pool || 'basic')])];
  const opts = [
    `<option value="auto">${xe(tr('account.pool.autoRecommended', null, '自动检测（推荐）'))}</option>`,
    ...known.map(p => `<option value="${p}">${xe(poolLabel(p))}</option>`),
  ];
  document.getElementById(id).innerHTML = opts.join('');
}

// For edit modal: shows real pool names only (no "auto").
function fillPoolOptions(id) {
  const pools = [...new Set(['basic', 'super', 'heavy', ...allTokens.map(t => t.pool || 'basic')])];
  document.getElementById(id).innerHTML = pools.map(p => `<option value="${p}">${xe(poolLabel(p))}</option>`).join('');
}

function openAdd() {
  document.getElementById('import-tokens').value = '';
  fillImportPoolOptions('import-pool');
  openModal('modal-import');
}

function openImport() {
  document.getElementById('import-file').value = '';
  updateImportFileState();
  fillImportPoolOptions('import-file-pool');
  openModal('modal-import-file');
}

function openEdit(token) {
  const item = allTokens.find(t => t.token === token);
  if (!item) return showToast(tr('account.notFound', null, '账户不存在'), 'error');
  _editingToken = item.token;
  document.getElementById('edit-token').value = item.token;
  fillPoolOptions('edit-pool');
  document.getElementById('edit-pool').value = item.pool || 'basic';
  openModal('modal-edit');
}

async function doAdd() {
  const raw  = document.getElementById('import-tokens').value;
  const pool = document.getElementById('import-pool').value;
  const autoNsfw = document.getElementById('import-auto-nsfw').checked;
  const newToks = raw.split('\n').map(s => s.trim()).filter(Boolean);
  if (!newToks.length) return showToast(tr('account.enterToken', null, '请输入 Token'), 'error');
  return saveTokens(
    pool,
    newToks,
    autoNsfw,
    'modal-import',
    tr('account.adding', { n: newToks.length }, `正在添加 ${newToks.length} 个账户…`),
    tr('account.addDone', null, '添加完成'),
    tr('account.addFailed', null, '添加失败'),
  );
}

async function doImport() {
  const file = document.getElementById('import-file').files?.[0];
  if (!file) return showToast(tr('account.selectFile', null, '请选择文件'), 'error');
  const isJson = file.name.toLowerCase().endsWith('.json');
  const autoNsfw = document.getElementById('import-file-auto-nsfw').checked;
  const form = new FormData();
  form.append('file', file);
  form.append('mode', isJson ? 'replace' : 'add');
  form.append('auto_nsfw', autoNsfw ? 'true' : 'false');
  if (!isJson) form.append('pool', document.getElementById('import-file-pool').value);
  closeModal('modal-import-file');
  return _runTask(
    () => _api('POST', '/tokens/import-async', form),
    tr('account.importingFile', null, '正在导入文件…'),
    null,
    0,
  );
}

async function doEdit() {
  const token = document.getElementById('edit-token').value.trim();
  const pool = document.getElementById('edit-pool').value;
  if (!_editingToken) return;
  if (!token) return showToast(tr('account.enterToken', null, '请输入 Token'), 'error');
  try {
    showToast(tr('account.saving', null, '正在保存修改…'), 'info');
    await _api('PUT', '/tokens/edit', { old_token: _editingToken, token, pool });
    closeModal('modal-edit');
    _editingToken = '';
    showToast(tr('account.editDone', null, '编辑完成'), 'success');
    load();
  } catch (e) { showToast(`${tr('account.editFailed', null, '编辑失败')}: ${e.message}`, 'error'); }
}

function updateImportFileState() {
  const file = document.getElementById('import-file').files?.[0];
  const el = document.getElementById('import-file-name');
  if (!el) return;
  el.textContent = file ? file.name : tr('account.noFile', null, '未选择文件');
  el.classList.toggle('empty', !file);
  const isJson = file && file.name.toLowerCase().endsWith('.json');
  const poolRow = document.getElementById('import-file-pool-row');
  const hint    = document.getElementById('import-file-hint');
  if (poolRow) poolRow.style.display = isJson ? 'none' : '';
  if (hint) hint.textContent = isJson
    ? tr('account.importHintJson', null, 'JSON 文件已包含 pool 信息，无需选择类型')
    : tr('account.importHintTxt', null, '每行一个 Token，将添加到所选类型');
}

async function saveTokens(pool, newToks, autoNsfw, modalId, pendingMessage, successPrefix, errorPrefix) {
  const unique = [...new Set(newToks)];
  try {
    const form = new FormData();
    form.append('pool', pool);
    form.append('mode', 'add');
    form.append('tokens_text', unique.join('\n'));
    closeModal(modalId);
    await _runTask(
      () => _api('POST', `/tokens/import-async?auto_nsfw=${autoNsfw ? 'true' : 'false'}`, form),
      pendingMessage,
      (_ok, _fail, summary) => {
        const skipped = summary?.skipped ?? 0;
        if (skipped) showToast(tr('account.saveResultSkipped', { prefix: successPrefix, count: summary.ok || 0, skipped }, `${successPrefix}，新增 ${summary.ok || 0}，已存在跳过 ${skipped}`), 'success');
      },
      unique.length,
    );
  } catch (e) { showToast(`${errorPrefix}: ${e.message}`, 'error'); }
}

// ── Export ─────────────────────────────────────────────────────────────────
async function fetchAllFilteredTokens() {
  const out = [];
  let page = 1;
  while (true) {
    const params = buildTokenQuery({ page, size: 2000 });
    const data = await _api('GET', `/tokens?${params}`);
    const items = Array.isArray(data.tokens) ? data.tokens : [];
    out.push(...items);
    const meta = data.pagination || {};
    if (!items.length || page >= (meta.total_pages || 1)) break;
    page += 1;
  }
  return out;
}

async function doExport() {
  let tokens = [];
  try {
    showToast(tr('account.exportLoading', null, '正在准备导出…'), 'info');
    tokens = await fetchAllFilteredTokens();
  } catch (e) {
    return showToast(`${tr('account.exportFailed', null, '导出失败')}: ${e.message}`, 'error');
  }
  if (!tokens.length) return showToast(tr('account.exportEmpty', null, '当前没有可导出的 Token'), 'error');
  openModal('modal-export');
  document.getElementById('exp-count').textContent = tokens.length;
  document.getElementById('exp-json-btn').onclick = () => {
    closeModal('modal-export');
    const out = {};
    tokens.forEach(t => (out[t.pool||'basic'] = out[t.pool||'basic']||[]).push({token:t.token,tags:t.tags||[]}));
    _download(JSON.stringify(out,null,2), `tokens-${currentFilterSuffix()}.json`, 'application/json');
  };
  document.getElementById('exp-txt-btn').onclick = () => {
    closeModal('modal-export');
    _download(tokens.map(t => t.token).join('\n'), `tokens-${currentFilterSuffix()}.txt`, 'text/plain');
  };
}

function currentFilterSuffix() {
  return [curStatus, curNsfw, curPool].filter(Boolean).join('-');
}

function _download(content, filename, mime) {
  const a = document.createElement('a');
  a.href = URL.createObjectURL(new Blob([content], {type: mime}));
  a.download = filename; a.click();
}

// ── Delete ─────────────────────────────────────────────────────────────────
function doDelete(tokens) {
  const n = tokens.length;
  openConfirm(tr('account.deleteConfirmTitle', null, '确认删除'),
    n === 1
      ? tr('account.deleteConfirmOne', { token: `<code>${mask(tokens[0])}</code>` }, `确认删除 <code>${mask(tokens[0])}</code>？<br><small style="color:var(--error)">此操作不可撤销。</small>`)
      : tr('account.deleteConfirmMany', { n }, `确认删除 <b>${n}</b> 个账户？<br><small style="color:var(--error)">此操作不可撤销。</small>`),
    async () => {
      try {
        showToast(tr('account.deleting', { n }, `正在删除 ${n} 个账户…`), 'info');
        await _api('DELETE', '/tokens', tokens);
        tokens.forEach(t => sel.delete(t));
        showToast(tr('account.deleteDone', { n }, `已删除 ${n} 个`), 'success');
        load();
      } catch (e) { showToast(`${tr('account.deleteFailed', null, '删除失败')}: ${e.message}`, 'error'); }
    });
}
function batchDeleteSel() { if (sel.size) doDelete([...sel]); }

function deleteAllInvalid() {
  const tokens = allTokens.filter(t => isInvalidStatus(t.status)).map(t => t.token);
  if (!tokens.length) return showToast(tr('account.noInvalidAccounts', null, '没有异常账户可删除'), 'error');
  doDelete(tokens);
}

// ── Batch SSE runner ───────────────────────────────────────────────────────
let _batchTaskId = null;
let _batchEs     = null;

async function _runTask(starter, label, onDone, totalFallback = 0) {
  const btnCancel = document.getElementById('btn-batch-cancel');
  ['btn-export','btn-nsfw-all','btn-refresh-all','btn-delete-invalid-all','page-size-sel','btn-nsfw','btn-refresh','btn-disable','btn-restore','btn-delete'].forEach(id =>
    document.getElementById(id).style.display = 'none');
  btnCancel.style.display = '';

  const progress = showProgressToast(label);
  let _finalReceived = false;

  function _cleanup() {
    btnCancel.style.display = 'none';
    updateBatchBtns();
  }
  function _done(es) {
    _finalReceived = true;
    es.close(); _batchEs = null;
    _cleanup();
  }

  try {
    const d = await starter();
    _batchTaskId = d.task_id;
    const total = d.total || totalFallback;

    const controller = new AbortController();
    const es = { close: () => controller.abort() };
    _batchEs = es;

    const onStreamEvent = (ev) => {
      if (ev.type === 'snapshot' || ev.type === 'progress') {
        progress.update(ev.processed || 0, total);
      }
      if (['done', 'error', 'cancelled'].includes(ev.type)) {
        _done(es);
        if (ev.type === 'done') {
          const s = ev.result?.summary || {};
          const ok = s.ok ?? 0, fail = s.fail ?? 0;
          progress.finish(`${label.replace('…','').replace('正在','')} 完成：成功 ${ok}，失败 ${fail}`, fail > 0 ? 'error' : 'success');
          onDone?.(ok, fail, s);
          load();
        } else if (ev.type === 'cancelled') {
          progress.finish('已取消', 'error');
        } else {
          progress.finish(ev.error || '操作失败', 'error');
        }
      }
    };
    window.AdminAccountModules.api.streamAdminSSE(`${ADMIN_API}/batch/${_batchTaskId}/stream`, {
      signal: controller.signal,
      onEvent: onStreamEvent,
    }).then(() => {
      if (!_finalReceived) {
        _batchEs = null;
        _cleanup();
        progress.finish('连接中断', 'error');
      }
    }).catch((error) => {
      if (_finalReceived || controller.signal.aborted) { _batchEs = null; return; }
      _batchEs = null;
      _cleanup();
      progress.finish(error?.message || '连接中断', 'error');
    });
  } catch (e) {
    _cleanup();
    progress.finish(`启动失败: ${e.message}`, 'error');
  }
}

async function _runBatch(endpoint, tokens, label, onDone) {
  return _runTask(
    () => _api('POST', appendQuery(endpoint, 'async=true'), { tokens }),
    label,
    onDone,
    tokens.length,
  );
}

function appendQuery(url, param) {
  return `${url}${url.includes('?') ? '&' : '?'}${param}`;
}

async function cancelBatch() {
  if (!_batchTaskId) return;
  showToast('已暂停，请耐心等待本批次完成…', 'info');
  try { await _api('POST', `/batch/${_batchTaskId}/cancel`); } catch {}
}

// ── Refresh ────────────────────────────────────────────────────────────────
async function refreshOne(token) {
  if (refreshingTokens.has(token)) return;
  refreshingTokens.add(token);
  renderTable();
  try {
    showToast(tr('account.refreshing', { n: 1 }, '正在刷新 1 个账户…'), 'info');
    const d = await _api('POST', '/batch/refresh', { tokens:[token] });
    showToast(tr('account.refreshDone', { ok: d.summary?.ok ?? 0, fail: d.summary?.fail ?? 0 }, `刷新完成：成功 ${d.summary?.ok??0}，失败 ${d.summary?.fail??0}`), 'success');
    await load();
  } catch (e) { showToast(`${tr('account.refreshFailed', null, '刷新失败')}: ${e.message}`, 'error'); }
  finally {
    refreshingTokens.delete(token);
    renderTable();
  }
}

async function setDisabled(token, disabled) {
  const masked = mask(token);
  const title = disabled
    ? tr('account.disableConfirmTitle', null, '禁用账号')
    : tr('account.restoreConfirmTitle', null, '恢复账号');
  const body = disabled
    ? tr('account.disableConfirmBody', { token: `<code>${masked}</code>` }, `确认禁用 <code>${masked}</code>？<br><small style="color:var(--fg-muted)">禁用后该账号不会参与请求分配。</small>`)
    : tr('account.restoreConfirmBody', { token: `<code>${masked}</code>` }, `确认恢复 <code>${masked}</code>？<br><small style="color:var(--fg-muted)">恢复后该账号将重新参与请求分配。</small>`);

  openConfirm(title, body, async () => {
    try {
      showToast(
        disabled
          ? tr('account.disablingOne', null, '正在禁用账号…')
          : tr('account.restoringOne', null, '正在恢复账号…'),
        'info',
      );
      await _api('POST', '/tokens/disabled', { token, disabled });
      showToast(
        disabled
          ? tr('account.disableDone', null, '账号已禁用')
          : tr('account.restoreDone', null, '账号已恢复'),
        'success',
      );
      await load();
    } catch (e) {
      showToast(`${tr('account.operationFailed', null, '操作失败')}: ${e.message}`, 'error');
    }
  });
}

function disableOne(token) { setDisabled(token, true); }
function restoreOne(token) { setDisabled(token, false); }

function batchSetDisabled(disabled) {
  if (!sel.size) return;
  const tokens = [...sel];
  const n = tokens.length;
  const title = disabled
    ? tr('account.batchDisableConfirmTitle', null, '批量禁用账号')
    : tr('account.batchRestoreConfirmTitle', null, '批量恢复账号');
  const body = disabled
    ? tr('account.batchDisableConfirmBody', { n }, `确认禁用选中的 <b>${n}</b> 个账户？<br><small style="color:var(--fg-muted)">禁用后这些账号不会参与请求分配，但可随时恢复。</small>`)
    : tr('account.batchRestoreConfirmBody', { n }, `确认恢复选中的 <b>${n}</b> 个账户？<br><small style="color:var(--fg-muted)">恢复后这些账号将重新参与请求分配。</small>`);

  openConfirm(title, body, async () => {
    try {
      showToast(
        disabled
          ? tr('account.disablingMany', { n }, `正在禁用 ${n} 个账户…`)
          : tr('account.restoringMany', { n }, `正在恢复 ${n} 个账户…`),
        'info',
      );
      const d = await _api('POST', '/tokens/disabled/batch', { tokens, disabled });
      const ok = d.summary?.ok ?? 0;
      const fail = d.summary?.fail ?? 0;
      showToast(
        disabled
          ? tr('account.disableManyDone', { ok, fail }, `禁用完成：成功 ${ok} 个，失败 ${fail} 个`)
          : tr('account.restoreManyDone', { ok, fail }, `恢复完成：成功 ${ok} 个，失败 ${fail} 个`),
        fail > 0 ? 'error' : 'success',
      );
      sel.clear();
      await load();
    } catch (e) {
      showToast(`${tr('account.operationFailed', null, '操作失败')}: ${e.message}`, 'error');
    }
  });
}

function batchDisableSel() { batchSetDisabled(true); }
function batchRestoreSel() { batchSetDisabled(false); }

async function batchRefreshSel() {
  if (!sel.size) return;
  await _runBatch('/batch/refresh', [...sel],
    tr('account.refreshing', { n: sel.size }, `正在刷新 ${sel.size} 个账户…`),
    showRefreshBatchResult,
  );
}

function showRefreshBatchResult(ok, fail, summary) {
  const expired = summary?.expired ?? 0;
  const transient = summary?.transient ?? Math.max(0, fail - expired);
  let msg = `刷新完成：成功 ${ok}`;
  if (expired > 0) msg += `，异常 ${expired}`;
  if (transient > 0) msg += `，临时失败 ${transient}`;
  showToast(msg, fail > 0 ? 'warning' : 'success');
}

function manageableTokens() {
  return allTokens.filter(t => isManageableStatus(t.status)).map(t => t.token);
}

function selectedManageableTokens() {
  return allTokens.filter(t => sel.has(t.token) && isManageableStatus(t.status)).map(t => t.token);
}

function batchRefreshAllManageable() {
  const n = manageableTokens().length;
  if (!n) return showToast(tr('account.noManageableAccounts', null, '没有可操作的可用账户'), 'error');
  openConfirm(
    tr('account.refreshAllConfirmTitle', null, '重新测试全部正常账户'),
    tr('account.refreshAllConfirmBody', { n }, `重新测试全部 <b>${n}</b> 个正常/限流账户的运行状态？<br><small style="color:var(--fg-muted)">该操作会调用上游接口，并按当前批量并发配置执行。</small>`),
    async () => _runBatch('/batch/refresh?all_manageable=true', [],
      tr('account.refreshAllRunning', { n }, `正在重新测试 ${n} 个账户…`),
      showRefreshBatchResult,
    )
  );
}

// ── NSFW ───────────────────────────────────────────────────────────────────
async function nsfwOne(token, enabled = true) {
  try {
    showToast(enabled
      ? tr('account.nsfwEnablingOne', null, '正在开启 NSFW…')
      : tr('account.nsfwDisablingOne', null, '正在关闭 NSFW…'), 'info');
    const d = await _api('POST', `/batch/nsfw?enabled=${enabled ? 'true' : 'false'}`, { tokens: [token] });
    showToast(enabled
      ? tr('account.nsfwDone', { ok: d.summary?.ok ?? 0, fail: d.summary?.fail ?? 0 }, `NSFW 已开启：成功 ${d.summary?.ok??0}，失败 ${d.summary?.fail??0}`)
      : tr('account.nsfwDisableDone', { ok: d.summary?.ok ?? 0, fail: d.summary?.fail ?? 0 }, `NSFW 已关闭：成功 ${d.summary?.ok??0}，失败 ${d.summary?.fail??0}`),
      'success');
    load();
  } catch (e) { showToast(`${tr('account.operationFailed', null, '操作失败')}: ${e.message}`, 'error'); }
}

function batchNSFW() {
  if (!sel.size) return;
  const tokens = selectedManageableTokens();
  if (!tokens.length) return showToast(tr('account.noManageableAccounts', null, '没有可操作的可用账户'), 'error');
  openConfirm(
    tr('account.nsfwConfirmTitle', null, '开启 NSFW'),
    tr('account.nsfwConfirmBody', { n: tokens.length }, `为选中的 <b>${tokens.length}</b> 个正常/限流账户开启 NSFW？`),
    async () => _runBatch('/batch/nsfw', tokens,
      tr('account.nsfwEnabling', { n: tokens.length }, `正在为 ${tokens.length} 个账户开启 NSFW…`),
      (ok, fail) => showToast(tr('account.nsfwDone', { ok, fail }, `NSFW 完成：成功 ${ok}，失败 ${fail}`), 'success')
    )
  );
}

function batchNSFWAll() {
  const n = manageableTokens().length;
  if (!n) return showToast(tr('account.noManageableAccounts', null, '没有可操作的可用账户'), 'error');
  openConfirm(
    tr('account.nsfwAllConfirmTitle', null, '全部开启 NSFW'),
    tr('account.nsfwAllConfirmBody', { n }, `为全部 <b>${n}</b> 个正常/限流账户开启 NSFW？<br><small style="color:var(--fg-muted)">该操作会按当前批量并发配置执行。</small>`),
    async () => _runBatch('/batch/nsfw?all_manageable=true', [],
      tr('account.nsfwAllEnabling', { n }, `正在为 ${n} 个账户开启 NSFW…`),
      (ok, fail) => showToast(tr('account.nsfwDone', { ok, fail }, `NSFW 完成：成功 ${ok}，失败 ${fail}`), fail > 0 ? 'error' : 'success')
    )
  );
}

// ── Modal ──────────────────────────────────────────────────────────────────
function openModal(id)  { document.getElementById(id).classList.add('open'); }
function closeModal(id) { document.getElementById(id).classList.remove('open'); }
function openConfirm(title, body, cb) {
  _cb = async () => { closeModal('modal-confirm'); await cb(); };
  document.getElementById('confirm-title').textContent = title;
  document.getElementById('confirm-body').innerHTML = body;
  openModal('modal-confirm');
}
document.querySelectorAll('.modal-overlay').forEach(el =>
  el.addEventListener('click', e => { if (e.target === el) el.classList.remove('open'); }));

const _tooltipEl = document.createElement('div');
_tooltipEl.className = 'admin-tooltip';
document.body.appendChild(_tooltipEl);
let _tooltipTarget = null;

function _placeTooltip(x, y) {
  const pad = 12;
  const rect = _tooltipEl.getBoundingClientRect();
  const maxX = window.innerWidth - rect.width - pad;
  const maxY = window.innerHeight - rect.height - pad;
  const left = Math.max(pad, Math.min(x + 14, maxX));
  const top = Math.max(pad, Math.min(y + 16, maxY));
  _tooltipEl.style.left = `${left}px`;
  _tooltipEl.style.top = `${top}px`;
}

function _showTooltip(target, x, y) {
  const text = target?.getAttribute('data-tip');
  if (!text) return;
  _tooltipTarget = target;
  _tooltipEl.textContent = text;
  _tooltipEl.classList.add('show');
  _placeTooltip(x, y);
}

function _hideTooltip() {
  _tooltipTarget = null;
  _tooltipEl.classList.remove('show');
}

document.addEventListener('mouseover', (e) => {
  const target = e.target.closest('[data-tip]');
  if (!target) return _hideTooltip();
  _showTooltip(target, e.clientX, e.clientY);
});

document.addEventListener('mousemove', (e) => {
  if (_tooltipTarget) _placeTooltip(e.clientX, e.clientY);
});

document.addEventListener('mouseout', (e) => {
  if (!_tooltipTarget) return;
  const next = e.relatedTarget;
  if (next && _tooltipTarget.contains(next)) return;
  if (e.target.closest('[data-tip]') === _tooltipTarget) _hideTooltip();
});

document.addEventListener('focusin', (e) => {
  const target = e.target.closest('[data-tip]');
  if (!target) return;
  const rect = target.getBoundingClientRect();
  _showTooltip(target, rect.left + rect.width / 2, rect.top);
});

document.addEventListener('focusout', (e) => {
  if (e.target.closest('[data-tip]')) _hideTooltip();
});

document.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-nsfw-token]');
  if (!btn) return;
  e.preventDefault();
  const token = btn.getAttribute('data-nsfw-token');
  const enabled = btn.getAttribute('data-nsfw-enabled') === 'true';
  if (token) nsfwOne(token, enabled);
});
