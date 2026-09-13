// smsc-sim Modern Web Admin Application Logic

(function () {
  'use strict';

  // Application State
  const state = {
    activeView: 'overview',
    pollInterval: 2000,
    isPaused: false,
    pollTimer: null,
    overview: null,
    sessions: [],
    events: [],
    config: null,
    moHistory: JSON.parse(localStorage.getItem('smsc_sim_mo_history') || '[]'),
    targetDisconnectId: null,
    operatorFilter: '',
    eventOpFilter: 'all',
    eventTypeFilter: 'all',
  };

  // DOM Elements
  const el = {
    navTabs: document.querySelectorAll('.nav-tab'),
    viewPanels: document.querySelectorAll('.view-panel'),
    uptimeVal: document.getElementById('telemetry-uptime'),
    statusLabel: document.getElementById('status-label'),
    pollIntervalSelect: document.getElementById('poll-interval-select'),
    toggleRefreshBtn: document.getElementById('toggle-refresh-btn'),
    refreshNowBtn: document.getElementById('refresh-now-btn'),
    pauseIcon: document.getElementById('pause-icon'),
    playIcon: document.getElementById('play-icon'),
    
    // KPI
    kpiOpsCount: document.getElementById('kpi-operators-count'),
    kpiBindsCount: document.getElementById('kpi-binds-count'),
    kpiBindsSub: document.getElementById('kpi-binds-sub'),
    kpiMsgsCount: document.getElementById('kpi-messages-count'),
    kpiThrottledCount: document.getElementById('kpi-throttled-count'),
    kpiThrottledSub: document.getElementById('kpi-throttled-sub'),
    kpiDlrStatus: document.getElementById('kpi-dlr-status'),
    
    // Badges
    badgeSessions: document.getElementById('badge-sessions-count'),
    badgeEvents: document.getElementById('badge-events-count'),
    
    // Fleet Overview
    filterOpInput: document.getElementById('filter-operator-input'),
    operatorsGrid: document.getElementById('operators-grid'),
    visibleOpsCount: document.getElementById('visible-operators-count'),
    totalOpsCount: document.getElementById('total-operators-count'),
    
    // Sessions
    sessionsTbody: document.getElementById('sessions-tbody'),
    sessionsEmpty: document.getElementById('sessions-empty-state'),
    btnRefreshSessions: document.getElementById('btn-refresh-sessions'),
    
    // MO Studio
    moForm: document.getElementById('mo-form'),
    moOpSelect: document.getElementById('mo-operator-select'),
    moSourceInput: document.getElementById('mo-source-input'),
    moDestInput: document.getElementById('mo-dest-input'),
    moPresetSelect: document.getElementById('mo-preset-select'),
    moEncodingSelect: document.getElementById('mo-encoding-select'),
    moTextInput: document.getElementById('mo-text-input'),
    moCharCounter: document.getElementById('mo-char-counter'),
    moSubmitBtn: document.getElementById('mo-submit-btn'),
    moResetBtn: document.getElementById('mo-reset-btn'),
    moHistoryTbody: document.getElementById('mo-history-tbody'),
    moEmptyHistory: document.getElementById('mo-empty-history'),
    moClearHistoryBtn: document.getElementById('mo-clear-history-btn'),
    
    // Events
    eventFilterOp: document.getElementById('event-filter-op'),
    eventFilterType: document.getElementById('event-filter-type'),
    eventsTbody: document.getElementById('events-tbody'),
    eventsEmpty: document.getElementById('events-empty-state'),
    btnClearEvents: document.getElementById('btn-clear-events'),
    
    // Config
    configJsonBlock: document.getElementById('config-json-block'),
    btnCopyConfig: document.getElementById('btn-copy-config'),
    
    // Modal
    disconnectModal: document.getElementById('disconnect-modal'),
    modalSessionId: document.getElementById('modal-session-id'),
    modalCloseBtn: document.getElementById('modal-close-btn'),
    modalCancelBtn: document.getElementById('modal-cancel-btn'),
    modalConfirmBtn: document.getElementById('modal-confirm-btn'),
    
    // Toast
    toastContainer: document.getElementById('toast-container'),
  };

  // Preset Templates
  const presets = {
    custom: '',
    otp: 'Your verification code is 482091. Valid for 5 minutes. Do not share this code with anyone.',
    delivery: 'Your package #TK-94021 has arrived at the distribution center and is out for delivery today.',
    bank: 'BankAlert: Card *4829 charged $34.50 at Cafe. Available balance: $1,280.00.',
    promo: 'Flash Sale! 50% discount on all cloud services for the next 24 hours. Visit https://textup.io',
  };

  // Initialize
  function init() {
    setupEventListeners();
    setupRouting();
    renderMOHistory();
    startPolling();
    refreshAll();
  }

  // Routing and Tabs
  function setupRouting() {
    const hash = window.location.hash.replace('#', '');
    if (hash && ['overview', 'sessions', 'mo', 'events', 'config'].includes(hash)) {
      switchView(hash);
    }
  }

  function switchView(viewName) {
    state.activeView = viewName;
    window.location.hash = viewName;
    
    el.navTabs.forEach((tab) => {
      if (tab.dataset.view === viewName) {
        tab.classList.add('active');
      } else {
        tab.classList.remove('active');
      }
    });

    el.viewPanels.forEach((panel) => {
      if (panel.id === `view-${viewName}`) {
        panel.classList.add('active');
      } else {
        panel.classList.remove('active');
      }
    });

    if (viewName === 'config' && !state.config) {
      fetchConfig();
    }
  }

  // Event Listeners
  function setupEventListeners() {
    // Nav tabs
    el.navTabs.forEach((tab) => {
      tab.addEventListener('click', () => switchView(tab.dataset.view));
    });

    // Refresh controls
    el.pollIntervalSelect.addEventListener('change', (e) => {
      const val = parseInt(e.target.value, 10);
      state.pollInterval = val;
      if (val === 0) {
        pausePolling();
      } else {
        state.isPaused = false;
        updatePauseUI();
        startPolling();
      }
    });

    el.toggleRefreshBtn.addEventListener('click', () => {
      if (state.isPaused) {
        state.isPaused = false;
        if (state.pollInterval === 0) {
          state.pollInterval = 2000;
          el.pollIntervalSelect.value = '2000';
        }
        startPolling();
      } else {
        pausePolling();
      }
      updatePauseUI();
    });

    el.refreshNowBtn.addEventListener('click', () => {
      spinRefreshIcon();
      refreshAll();
    });

    // Sessions refresh
    el.btnRefreshSessions.addEventListener('click', () => {
      fetchSessions();
      showToast('Sessions refreshed', 'success');
    });

    // Filter operators
    el.filterOpInput.addEventListener('input', (e) => {
      state.operatorFilter = e.target.value.toLowerCase().trim();
      renderOperators();
    });

    // Event filters
    el.eventFilterOp.addEventListener('change', (e) => {
      state.eventOpFilter = e.target.value;
      renderEvents();
    });

    el.eventFilterType.addEventListener('change', (e) => {
      state.eventTypeFilter = e.target.value;
      renderEvents();
    });

    el.btnClearEvents.addEventListener('click', () => {
      state.events = [];
      renderEvents();
      showToast('Activity log cleared', 'info');
    });

    // MO Form
    el.moPresetSelect.addEventListener('change', (e) => {
      const val = e.target.value;
      if (presets[val] !== undefined) {
        el.moTextInput.value = presets[val];
        updateCharCounter();
      }
    });

    el.moTextInput.addEventListener('input', updateCharCounter);

    el.moResetBtn.addEventListener('click', () => {
      el.moForm.reset();
      el.moPresetSelect.value = 'custom';
      updateCharCounter();
    });

    el.moForm.addEventListener('submit', handleMOSubmit);

    el.moClearHistoryBtn.addEventListener('click', () => {
      state.moHistory = [];
      localStorage.removeItem('smsc_sim_mo_history');
      renderMOHistory();
    });

    // Copy Config
    el.btnCopyConfig.addEventListener('click', () => {
      if (!state.config) return;
      navigator.clipboard.writeText(JSON.stringify(state.config, null, 2)).then(() => {
        showToast('Configuration copied to clipboard', 'success');
      });
    });

    // Modal
    el.modalCloseBtn.addEventListener('click', closeModal);
    el.modalCancelBtn.addEventListener('click', closeModal);
    el.disconnectModal.addEventListener('click', (e) => {
      if (e.target === el.disconnectModal) closeModal();
    });
    el.modalConfirmBtn.addEventListener('click', confirmDisconnect);
  }

  function updatePauseUI() {
    if (state.isPaused) {
      el.pauseIcon.classList.add('hidden');
      el.playIcon.classList.remove('hidden');
    } else {
      el.pauseIcon.classList.remove('hidden');
      el.playIcon.classList.add('hidden');
    }
  }

  function spinRefreshIcon() {
    const icon = el.refreshNowBtn.querySelector('.refresh-icon');
    icon.classList.add('spinning');
    setTimeout(() => icon.classList.remove('spinning'), 600);
  }

  // Polling
  function startPolling() {
    if (state.pollTimer) clearInterval(state.pollTimer);
    if (state.pollInterval <= 0 || state.isPaused) return;

    state.pollTimer = setInterval(() => {
      if (!state.isPaused) refreshAll();
    }, state.pollInterval);
  }

  function pausePolling() {
    state.isPaused = true;
    if (state.pollTimer) clearInterval(state.pollTimer);
    updatePauseUI();
  }

  // Data Fetching
  async function refreshAll() {
    try {
      await Promise.all([
        fetchOverview(),
        fetchSessions(),
        fetchEvents(),
      ]);
      el.statusLabel.textContent = 'ONLINE';
    } catch (err) {
      console.error('Failed to refresh data:', err);
      el.statusLabel.textContent = 'OFFLINE';
    }
  }

  async function fetchOverview() {
    const res = await fetch('/admin/overview');
    if (!res.ok) throw new Error('Overview HTTP ' + res.status);
    const data = await res.json();
    state.overview = data;
    renderKPIs();
    renderOperators();
    populateMOOperators();
    populateEventOperators();
  }

  async function fetchSessions() {
    const res = await fetch('/admin/sessions');
    if (!res.ok) throw new Error('Sessions HTTP ' + res.status);
    const data = await res.json();
    state.sessions = Array.isArray(data) ? data : [];
    renderSessions();
    updateSessionKPIs();
  }

  async function fetchEvents() {
    const res = await fetch('/admin/events?limit=150');
    if (!res.ok) throw new Error('Events HTTP ' + res.status);
    const data = await res.json();
    state.events = Array.isArray(data) ? data : [];
    renderEvents();
  }

  async function fetchConfig() {
    try {
      const res = await fetch('/admin/config');
      if (!res.ok) throw new Error('Config HTTP ' + res.status);
      const data = await res.json();
      state.config = data;
      el.configJsonBlock.innerHTML = `<code>${escapeHTML(JSON.stringify(data, null, 2))}</code>`;
    } catch (err) {
      el.configJsonBlock.innerHTML = `<code>Error loading config: ${escapeHTML(err.message)}</code>`;
    }
  }

  // Rendering Functions
  function renderKPIs() {
    if (!state.overview) return;
    const ov = state.overview;
    el.uptimeVal.textContent = ov.uptime || '0s';
    el.kpiOpsCount.textContent = ov.operators_count || 0;
    el.kpiMsgsCount.textContent = (ov.total_messages_seen || 0).toLocaleString();
    el.kpiThrottledCount.textContent = ov.throttled_operators || 0;
    el.kpiThrottledSub.textContent = `${ov.throttled_operators} / ${ov.operators_count} with rate limits`;
    el.kpiDlrStatus.textContent = ov.dlr_enabled_operators > 0 ? 'Active' : 'Disabled';
    el.kpiDlrStatus.style.color = ov.dlr_enabled_operators > 0 ? 'var(--cyan)' : 'var(--text-muted)';
  }

  function updateSessionKPIs() {
    const count = state.sessions.length;
    el.kpiBindsCount.textContent = count;
    el.badgeSessions.textContent = count;

    let tx = 0, rx = 0, trx = 0;
    state.sessions.forEach((s) => {
      const m = (s.mode || '').toLowerCase();
      if (m === 'tx') tx++;
      else if (m === 'rx') rx++;
      else if (m === 'trx') trx++;
    });
    el.kpiBindsSub.textContent = `TX: ${tx} | RX: ${rx} | TRX: ${trx}`;
  }

  function renderOperators() {
    if (!state.overview || !state.overview.operators) return;
    const allOps = state.overview.operators;
    el.totalOpsCount.textContent = allOps.length;

    const query = state.operatorFilter;
    const filtered = allOps.filter((op) => {
      if (!query) return true;
      return (
        op.name.toLowerCase().includes(query) ||
        op.listen.toLowerCase().includes(query) ||
        (op.accounts && op.accounts.some((a) => a.toLowerCase().includes(query)))
      );
    });

    el.visibleOpsCount.textContent = filtered.length;
    el.operatorsGrid.innerHTML = '';

    if (filtered.length === 0) {
      el.operatorsGrid.innerHTML = `
        <div class="empty-state" style="grid-column: 1 / -1;">
          <div class="empty-icon">🔍</div>
          <h3>No matching operators</h3>
          <p>No operator matches query "${escapeHTML(query)}".</p>
        </div>
      `;
      return;
    }

    filtered.forEach((op) => {
      const card = document.createElement('div');
      card.className = 'operator-card';
      
      const isOnline = true; // listeners are active
      const hasBinds = op.active_binds > 0;
      const bindClass = hasBinds ? 'badge-active' : 'badge-inactive';
      
      let throttleText = 'Unlimited';
      if (op.rate_limited) {
        throttleText = `${op.throttle_tps || 0} TPS (Burst ${op.throttle_burst || 0})`;
      }

      const accountsChips = (op.accounts || [])
        .map((a) => `<span class="account-chip">${escapeHTML(a)}</span>`)
        .join('');

      const bindTypes = (op.bind_types || []).map((b) => b.toUpperCase()).join(', ');

      card.innerHTML = `
        <div class="op-card-header">
          <div class="op-title-wrap">
            <span class="op-name">${escapeHTML(op.name)}</span>
          </div>
          <div class="op-badges">
            <span class="badge badge-port">${escapeHTML(op.listen)}</span>
            <span class="badge badge-version">v${escapeHTML(op.smpp_version)}</span>
            <span class="badge ${bindClass}">
              ${op.active_binds} ${op.active_binds === 1 ? 'bind' : 'binds'}
            </span>
          </div>
        </div>

        <div class="op-spec-grid">
          <div class="spec-item">
            <span class="spec-label">Rate Limit</span>
            <span class="spec-val">${escapeHTML(throttleText)}</span>
          </div>
          <div class="spec-item">
            <span class="spec-label">Window Size</span>
            <span class="spec-val">${op.window_size > 0 ? op.window_size + ' msg' : 'Unlimited'}</span>
          </div>
          <div class="spec-item">
            <span class="spec-label">Bind Modes</span>
            <span class="spec-val">${escapeHTML(bindTypes || 'ALL')}</span>
          </div>
          <div class="spec-item">
            <span class="spec-label">DLR Engine</span>
            <span class="spec-val" style="color: ${op.dlr_enabled ? 'var(--emerald)' : 'var(--text-muted)'};">
              ${op.dlr_enabled ? 'Enabled' : 'Disabled'}
            </span>
          </div>
        </div>

        <div class="op-accounts-wrap">
          <div class="spec-label" style="margin-bottom: 6px;">Configured Accounts</div>
          <div class="op-accounts">${accountsChips || '<span class="text-muted">None</span>'}</div>
        </div>

        <div class="op-card-footer">
          <div class="op-msgs-count">
            Messages: <strong>${(op.messages_seen || 0).toLocaleString()}</strong>
          </div>
          <button class="btn btn-secondary btn-xs btn-inject-mo" data-operator="${escapeHTML(op.name)}">
            <svg class="icon-sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"></path>
              <polyline points="22,6 12,13 2,6"></polyline>
            </svg>
            Inject MO
          </button>
        </div>
      `;

      card.querySelector('.btn-inject-mo').addEventListener('click', () => {
        openMOForOperator(op.name);
      });

      el.operatorsGrid.appendChild(card);
    });
  }

  function renderSessions() {
    const list = state.sessions;
    el.sessionsTbody.innerHTML = '';

    if (list.length === 0) {
      el.sessionsEmpty.classList.remove('hidden');
      return;
    }
    el.sessionsEmpty.classList.add('hidden');

    list.forEach((s) => {
      const tr = document.createElement('tr');
      const modeUpper = (s.mode || 'UNBOUND').toUpperCase();
      let modeBadge = 'badge-inactive';
      if (modeUpper === 'TRX') modeBadge = 'badge-active';
      else if (modeUpper === 'TX') modeBadge = 'badge-cyan';
      else if (modeUpper === 'RX') modeBadge = 'badge-warn';

      const connected = formatTimeAgo(s.connected_at);
      const lastRx = formatTimeAgo(s.last_rx_at);

      tr.innerHTML = `
        <td class="font-mono" style="font-weight: 600; color: #ffffff;">${escapeHTML(s.id)}</td>
        <td><span class="badge badge-port">${escapeHTML(s.operator)}</span></td>
        <td class="font-mono text-muted">${escapeHTML(s.remote_addr)}</td>
        <td class="font-mono"><strong>${escapeHTML(s.system_id || '—')}</strong></td>
        <td><span class="badge ${modeBadge}">${escapeHTML(modeUpper)}</span></td>
        <td class="font-mono">${s.in_flight || 0}</td>
        <td>${connected}</td>
        <td>${lastRx}</td>
        <td class="text-right">
          <button class="btn btn-danger btn-xs btn-disconnect-sess" data-id="${escapeHTML(s.id)}">
            Disconnect
          </button>
        </td>
      `;

      tr.querySelector('.btn-disconnect-sess').addEventListener('click', () => {
        openDisconnectModal(s.id);
      });

      el.sessionsTbody.appendChild(tr);
    });
  }

  function renderEvents() {
    const list = state.events;
    el.badgeEvents.textContent = list.length;
    el.eventsTbody.innerHTML = '';

    const opFilter = state.eventOpFilter;
    const typeFilter = state.eventTypeFilter;

    const filtered = list.filter((e) => {
      if (opFilter !== 'all' && e.operator !== opFilter) return false;
      if (typeFilter !== 'all' && e.type !== typeFilter) return false;
      return true;
    });

    if (filtered.length === 0) {
      el.eventsEmpty.classList.remove('hidden');
      return;
    }
    el.eventsEmpty.classList.add('hidden');

    filtered.forEach((e) => {
      const tr = document.createElement('tr');
      const timeStr = formatEventTime(e.time);
      
      let typeBadge = 'badge-port';
      if (e.level === 'success') typeBadge = 'badge-active';
      else if (e.level === 'warn') typeBadge = 'badge-warn';
      else if (e.level === 'error') typeBadge = 'badge-danger';
      else if (e.type === 'dlr') typeBadge = 'badge-cyan';

      tr.innerHTML = `
        <td class="font-mono text-muted">${timeStr}</td>
        <td><span class="badge badge-port">${escapeHTML(e.operator || 'system')}</span></td>
        <td><span class="badge ${typeBadge}">${escapeHTML((e.type || 'info').toUpperCase())}</span></td>
        <td style="color: #ffffff;">${escapeHTML(e.message)}</td>
        <td class="font-mono text-muted" style="font-size: 11px;">${escapeHTML(e.details || '—')}</td>
      `;

      el.eventsTbody.appendChild(tr);
    });
  }

  function populateMOOperators() {
    if (!state.overview || !state.overview.operators) return;
    const currentVal = el.moOpSelect.value;
    el.moOpSelect.innerHTML = '<option value="" disabled>Select an operator...</option>';

    // Count bound receivers per operator
    const receiverCounts = {};
    state.sessions.forEach((s) => {
      const m = (s.mode || '').toLowerCase();
      if (m === 'rx' || m === 'trx') {
        receiverCounts[s.operator] = (receiverCounts[s.operator] || 0) + 1;
      }
    });

    state.overview.operators.forEach((op) => {
      const opt = document.createElement('option');
      opt.value = op.name;
      const count = receiverCounts[op.name] || 0;
      const label = count > 0 
        ? `${op.name} (${count} active receiver ${count === 1 ? 'bind' : 'binds'})`
        : `${op.name} (no bound receiver)`;
      opt.textContent = label;
      el.moOpSelect.appendChild(opt);
    });

    if (currentVal && state.overview.operators.some((o) => o.name === currentVal)) {
      el.moOpSelect.value = currentVal;
    } else if (state.overview.operators.length > 0 && !el.moOpSelect.value) {
      // Pick first operator with bound receivers, or first operator
      const boundOp = state.overview.operators.find((o) => (receiverCounts[o.name] || 0) > 0);
      el.moOpSelect.value = boundOp ? boundOp.name : state.overview.operators[0].name;
    }
  }

  function populateEventOperators() {
    if (!state.overview || !state.overview.operators) return;
    const currentVal = el.eventFilterOp.value;
    el.eventFilterOp.innerHTML = '<option value="all">All Operators</option>';

    state.overview.operators.forEach((op) => {
      const opt = document.createElement('option');
      opt.value = op.name;
      opt.textContent = op.name;
      el.eventFilterOp.appendChild(opt);
    });

    if (currentVal) el.eventFilterOp.value = currentVal;
  }

  function openMOForOperator(opName) {
    switchView('mo');
    el.moOpSelect.value = opName;
    el.moTextInput.focus();
  }

  // MO Char Counter & UCS-2 Detection
  function updateCharCounter() {
    const text = el.moTextInput.value;
    const isUnicode = hasNonGSM(text);
    
    if (isUnicode && el.moEncodingSelect.value === '0') {
      el.moEncodingSelect.value = '8'; // switch to UCS-2
    }

    const coding = el.moEncodingSelect.value;
    let limit = 160;
    let concatLimit = 153;
    let type = 'GSM-7';

    if (coding === '8' || isUnicode) {
      limit = 70;
      concatLimit = 67;
      type = 'UCS-2';
    }

    const len = text.length;
    let parts = 1;
    if (len > limit) {
      parts = Math.ceil(len / concatLimit);
    }

    el.moCharCounter.textContent = `${len} / ${limit} chars (${parts} ${parts === 1 ? 'part' : 'parts'}, ${type})`;
  }

  function hasNonGSM(str) {
    // Check if string contains characters outside standard GSM 7-bit alphabet
    const gsm7 = "@£$¥èéùìòÇ\r\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#$%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà^{}\\[~]|€";
    for (let i = 0; i < str.length; i++) {
      if (gsm7.indexOf(str[i]) === -1) return true;
    }
    return false;
  }

  // Handle MO Submission
  async function handleMOSubmit(e) {
    e.preventDefault();
    const op = el.moOpSelect.value;
    const source = el.moSourceInput.value.trim();
    const dest = el.moDestInput.value.trim();
    const text = el.moTextInput.value.trim();
    const dataCoding = parseInt(el.moEncodingSelect.value, 10);

    if (!op) {
      showToast('Please select a target operator', 'error');
      return;
    }
    if (!dest || !text) {
      showToast('Recipient and message text are required', 'error');
      return;
    }

    el.moSubmitBtn.disabled = true;
    el.moSubmitBtn.innerHTML = `
      <svg class="icon-sm spinning" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"></path>
      </svg>
      Sending deliver_sm...
    `;

    try {
      const res = await fetch(`/admin/operators/${encodeURIComponent(op)}/mo`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          source: source,
          dest: dest,
          text: text,
          data_coding: dataCoding,
        }),
      });

      const body = await res.json();
      if (!res.ok) {
        throw new Error(body.error || `HTTP ${res.status}`);
      }

      showToast(`MO sent to ${dest} via ${op}`, 'success');
      
      // Add to history
      const item = {
        time: new Date().toISOString(),
        operator: op,
        source: source,
        dest: dest,
        text: text,
        status: 'Sent',
      };
      state.moHistory.unshift(item);
      if (state.moHistory.length > 25) state.moHistory.pop();
      localStorage.setItem('smsc_sim_mo_history', JSON.stringify(state.moHistory));
      renderMOHistory();

      // Refresh events
      fetchEvents();
    } catch (err) {
      showToast(`MO failed: ${err.message}`, 'error');
    } finally {
      el.moSubmitBtn.disabled = false;
      el.moSubmitBtn.innerHTML = `
        <svg class="icon-sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="22" y1="2" x2="11" y2="13"></line>
          <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
        </svg>
        Inject MO Deliver_SM
      `;
    }
  }

  function renderMOHistory() {
    el.moHistoryTbody.innerHTML = '';
    if (state.moHistory.length === 0) {
      el.moEmptyHistory.classList.remove('hidden');
      return;
    }
    el.moEmptyHistory.classList.add('hidden');

    state.moHistory.slice(0, 10).forEach((item) => {
      const tr = document.createElement('tr');
      const timeStr = formatEventTime(item.time);
      tr.innerHTML = `
        <td class="font-mono text-muted">${timeStr}</td>
        <td><span class="badge badge-port">${escapeHTML(item.operator)}</span></td>
        <td class="font-mono">${escapeHTML(item.dest)}</td>
        <td><span class="badge badge-active">${escapeHTML(item.status)}</span></td>
      `;
      el.moHistoryTbody.appendChild(tr);
    });
  }

  // Disconnect Modal
  function openDisconnectModal(sessionId) {
    state.targetDisconnectId = sessionId;
    el.modalSessionId.textContent = sessionId;
    el.disconnectModal.classList.remove('hidden');
  }

  function closeModal() {
    state.targetDisconnectId = null;
    el.disconnectModal.classList.add('hidden');
  }

  async function confirmDisconnect() {
    const id = state.targetDisconnectId;
    if (!id) return;

    el.modalConfirmBtn.disabled = true;
    try {
      const res = await fetch(`/admin/sessions/${encodeURIComponent(id)}/disconnect`, {
        method: 'POST',
      });
      if (!res.ok) {
        const body = await res.json();
        throw new Error(body.error || `HTTP ${res.status}`);
      }
      showToast(`Session ${id} disconnected`, 'warn');
      closeModal();
      await fetchSessions();
      await fetchEvents();
    } catch (err) {
      showToast(`Disconnect failed: ${err.message}`, 'error');
    } finally {
      el.modalConfirmBtn.disabled = false;
    }
  }

  // Toast System
  function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    
    let icon = 'ℹ️';
    if (type === 'success') icon = '✅';
    else if (type === 'error') icon = '❌';
    else if (type === 'warn') icon = '⚠️';

    toast.innerHTML = `
      <span class="toast-icon">${icon}</span>
      <span class="toast-text">${escapeHTML(message)}</span>
    `;

    el.toastContainer.appendChild(toast);

    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateX(100%)';
      toast.style.transition = 'all 0.3s ease-out';
      setTimeout(() => toast.remove(), 300);
    }, 4000);
  }

  // Utilities
  function escapeHTML(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  function formatTimeAgo(dateInput) {
    if (!dateInput) return '—';
    const date = new Date(dateInput);
    if (isNaN(date.getTime()) || date.getFullYear() < 2000) return '—';
    const seconds = Math.floor((new Date() - date) / 1000);
    if (seconds < 5) return 'just now';
    if (seconds < 60) return `${seconds}s ago`;
    const m = Math.floor(seconds / 60);
    if (m < 60) return `${m}m ${seconds % 60}s`;
    const h = Math.floor(m / 60);
    return `${h}h ${m % 60}m`;
  }

  function formatEventTime(dateInput) {
    if (!dateInput) return '—';
    const d = new Date(dateInput);
    if (isNaN(d.getTime())) return '—';
    return d.toTimeString().split(' ')[0] + '.' + String(d.getMilliseconds()).padStart(3, '0');
  }

  // Launch app
  document.addEventListener('DOMContentLoaded', init);
})();
