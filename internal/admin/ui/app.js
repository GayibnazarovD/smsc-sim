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
    targetDeleteOpName: null,
    operatorFilter: '',
    eventOpFilter: 'all',
    eventTypeFilter: 'all',
    currentUser: null,
    isAuthRequired: false,
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
    btnAddOperator: document.getElementById('btn-add-operator'),
    
    // Sessions
    sessionsTbody: document.getElementById('sessions-tbody'),
    sessionsEmpty: document.getElementById('sessions-empty-state'),
    btnRefreshSessions: document.getElementById('btn-refresh-sessions'),
    
    // MT Studio
    mtForm: document.getElementById('mt-form'),
    mtOpSelect: document.getElementById('mt-operator-select'),
    mtCommandSelect: document.getElementById('mt-command-select'),
    mtSourceInput: document.getElementById('mt-source-input'),
    mtDestInput: document.getElementById('mt-dest-input'),
    mtRegDelSelect: document.getElementById('mt-reg-del-select'),
    mtEncodingSelect: document.getElementById('mt-encoding-select'),
    mtTextInput: document.getElementById('mt-text-input'),
    mtCharCounter: document.getElementById('mt-char-counter'),
    mtSubmitBtn: document.getElementById('mt-submit-btn'),
    mtResetBtn: document.getElementById('mt-reset-btn'),
    mtResultStatusBadge: document.getElementById('mt-result-status-badge'),
    mtEmptyResult: document.getElementById('mt-empty-result'),
    mtInspectionResult: document.getElementById('mt-inspection-result'),
    resStatus: document.getElementById('res-status'),
    resLatency: document.getElementById('res-latency'),
    resMsgId: document.getElementById('res-msgid'),
    resCmd: document.getElementById('res-cmd'),
    resSrcAddr: document.getElementById('res-src-addr'),
    resSrcMeta: document.getElementById('res-src-meta'),
    resDestAddr: document.getElementById('res-dest-addr'),
    resDestMeta: document.getElementById('res-dest-meta'),
    resTlvSection: document.getElementById('res-tlv-section'),
    resTlvContent: document.getElementById('res-tlv-content'),
    resDlrSection: document.getElementById('res-dlr-section'),
    resDlrBox: document.getElementById('res-dlr-box'),
    resDlrBadge: document.getElementById('res-dlr-badge'),
    resDlrLatency: document.getElementById('res-dlr-latency'),
    resDlrText: document.getElementById('res-dlr-text'),

    // Virtual Receiver in MO
    btnToggleVR: document.getElementById('btn-toggle-vr'),
    vrStatusDot: document.getElementById('vr-status-dot'),
    vrStatusText: document.getElementById('vr-status-text'),
    vrInboxTbody: document.getElementById('vr-inbox-tbody'),
    vrEmptyInbox: document.getElementById('vr-empty-inbox'),
    vrClearInboxBtn: document.getElementById('vr-clear-inbox-btn'),

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
    
    // Disconnect Modal
    disconnectModal: document.getElementById('disconnect-modal'),
    modalSessionId: document.getElementById('modal-session-id'),
    modalCloseBtn: document.getElementById('modal-close-btn'),
    modalCancelBtn: document.getElementById('modal-cancel-btn'),
    modalConfirmBtn: document.getElementById('modal-confirm-btn'),

    // Add/Edit Operator Modal
    operatorModal: document.getElementById('operator-modal'),
    operatorModalTitle: document.getElementById('operator-modal-title'),
    opModalErrorAlert: document.getElementById('op-modal-error-alert'),
    opModalCloseBtn: document.getElementById('op-modal-close-btn'),
    opModalCancelBtn: document.getElementById('op-modal-cancel-btn'),
    opModalSaveBtn: document.getElementById('op-modal-save-btn'),
    operatorForm: document.getElementById('operator-form'),
    opFormIsEdit: document.getElementById('op-form-is-edit'),
    opFormOldName: document.getElementById('op-form-old-name'),
    opNameInput: document.getElementById('op-name-input'),
    opListenInput: document.getElementById('op-listen-input'),
    opVersionSelect: document.getElementById('op-version-select'),
    opWindowInput: document.getElementById('op-window-input'),
    btnAddAccount: document.getElementById('btn-add-account'),
    opAccountsContainer: document.getElementById('op-accounts-container'),
    opRateLimitedCheckbox: document.getElementById('op-rate-limited-checkbox'),
    opThrottleFields: document.getElementById('op-throttle-fields'),
    opTpsInput: document.getElementById('op-tps-input'),
    opBurstInput: document.getElementById('op-burst-input'),
    opDlrEnabledCheckbox: document.getElementById('op-dlr-enabled-checkbox'),
    opDlrFields: document.getElementById('op-dlr-fields'),
    opDlrMinInput: document.getElementById('op-dlr-min-input'),
    opDlrMaxInput: document.getElementById('op-dlr-max-input'),
    opFaultStatusSelect: document.getElementById('op-fault-status-select'),
    opFaultErrorPctInput: document.getElementById('op-fault-error-pct-input'),
    opFaultBindPctInput: document.getElementById('op-fault-bind-pct-input'),
    opFaultNackPctInput: document.getElementById('op-fault-nack-pct-input'),

    // Delete Operator Modal
    deleteOpModal: document.getElementById('delete-operator-modal'),
    delOpModalName: document.getElementById('del-op-modal-name'),
    delOpModalErrorAlert: document.getElementById('del-op-modal-error-alert'),
    delOpModalCloseBtn: document.getElementById('del-op-modal-close-btn'),
    delOpCancelBtn: document.getElementById('del-op-cancel-btn'),
    delOpConfirmBtn: document.getElementById('del-op-confirm-btn'),
    
    // Auth & User Elements
    userProfileBadge: document.getElementById('user-profile-badge'),
    navUserName: document.getElementById('nav-user-name'),
    btnChangePassword: document.getElementById('btn-change-password'),
    btnSignOut: document.getElementById('btn-sign-out'),
    authScreen: document.getElementById('auth-screen'),
    authTitle: document.getElementById('auth-title'),
    authSubtitle: document.getElementById('auth-subtitle'),
    authIconBadge: document.getElementById('auth-icon-badge'),
    authErrorAlert: document.getElementById('auth-error-alert'),
    setupForm: document.getElementById('setup-form'),
    setupUsernameInput: document.getElementById('setup-username-input'),
    setupPasswordInput: document.getElementById('setup-password-input'),
    setupConfirmInput: document.getElementById('setup-confirm-input'),
    setupSubmitBtn: document.getElementById('setup-submit-btn'),
    loginForm: document.getElementById('login-form'),
    loginUsernameInput: document.getElementById('login-username-input'),
    loginPasswordInput: document.getElementById('login-password-input'),
    loginSubmitBtn: document.getElementById('login-submit-btn'),
    changePasswordModal: document.getElementById('change-password-modal'),
    pwModalCloseBtn: document.getElementById('pw-modal-close-btn'),
    pwCancelBtn: document.getElementById('pw-cancel-btn'),
    pwSaveBtn: document.getElementById('pw-save-btn'),
    changePasswordForm: document.getElementById('change-password-form'),
    pwOldInput: document.getElementById('pw-old-input'),
    pwNewInput: document.getElementById('pw-new-input'),
    pwConfirmInput: document.getElementById('pw-confirm-input'),
    pwErrorAlert: document.getElementById('pw-error-alert'),
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

  async function loadStatuses() {
    try {
      const res = await apiFetch('/admin/statuses');
      if (!res.ok) return;
      const list = await res.json();
      if (!Array.isArray(list) || list.length === 0) return;

      const sel = el.opFaultStatusSelect;
      if (!sel) return;
      const currVal = sel.value;
      sel.innerHTML = '<option value="0">None / Success (0x00000000 - ESME_ROK)</option>';
      list.forEach((st) => {
        if (st.code === 0) return;
        const opt = document.createElement('option');
        opt.value = st.code;
        opt.textContent = `${st.hex} - ${st.name} (${st.description})`;
        sel.appendChild(opt);
      });
      if (currVal) sel.value = currVal;
    } catch (_) {}
  }

  async function init() {
    setupEventListeners();
    setupRouting();
    renderMOHistory();
    const isAuthed = await checkAuthStatus();
    if (isAuthed) {
      startPolling();
      refreshAll();
    }
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
    if (viewName === 'mo' && el.moOpSelect && el.moOpSelect.value) {
      checkVRStatus(el.moOpSelect.value);
    }
  }


  // Authentication & Security Handlers
  async function apiFetch(url, options = {}) {
    const res = await fetch(url, options);
    if (res.status === 401) {
      pausePolling();
      let errData = {};
      try {
        errData = await res.clone().json();
      } catch (_) {}
      if (errData.error === 'setup_required') {
        showAuthScreen('setup');
      } else {
        showAuthScreen('login');
      }
      throw new Error(errData.message || 'Unauthorized');
    }
    return res;
  }

  async function checkAuthStatus() {
    try {
      const res = await fetch('/admin/auth/status');
      if (!res.ok) throw new Error('Status HTTP ' + res.status);
      const data = await res.json();
      if (!data.initialized) {
        showAuthScreen('setup');
        return false;
      }
      if (!data.authenticated) {
        showAuthScreen('login');
        return false;
      }
      state.currentUser = data.user;
      updateUserBadge(data.user);
      hideAuthScreen();
      return true;
    } catch (err) {
      console.error('Check auth status failed:', err);
      return false;
    }
  }

  function showAuthScreen(type) {
    if (!el.authScreen) return;
    el.authScreen.classList.remove('hidden');
    el.authErrorAlert.classList.add('hidden');
    el.authErrorAlert.textContent = '';

    if (type === 'setup') {
      el.authTitle.textContent = 'First-Time Deployment Setup';
      el.authSubtitle.textContent = 'Configure the master administrator account to secure your SMSC Simulator instance.';
      el.authIconBadge.textContent = '🛡️';
      el.setupForm.classList.remove('hidden');
      el.loginForm.classList.add('hidden');
    } else {
      el.authTitle.textContent = 'Sign In to SMSC Simulator';
      el.authSubtitle.textContent = 'Enter your administrator credentials to access the console.';
      el.authIconBadge.textContent = '📡';
      el.setupForm.classList.add('hidden');
      el.loginForm.classList.remove('hidden');
    }
  }

  function hideAuthScreen() {
    if (el.authScreen) {
      el.authScreen.classList.add('hidden');
    }
  }

  function updateUserBadge(user) {
    if (!el.userProfileBadge) return;
    if (user && user.username) {
      el.navUserName.textContent = user.username;
      el.userProfileBadge.classList.remove('hidden');
    } else {
      el.userProfileBadge.classList.add('hidden');
    }
  }

  async function handleSetupSubmit(e) {
    e.preventDefault();
    el.authErrorAlert.classList.add('hidden');

    const username = el.setupUsernameInput.value.trim();
    const password = el.setupPasswordInput.value;
    const confirmPassword = el.setupConfirmInput.value;

    if (password !== confirmPassword) {
      el.authErrorAlert.textContent = 'Passwords do not match.';
      el.authErrorAlert.classList.remove('hidden');
      return;
    }
    if (password.length < 8) {
      el.authErrorAlert.textContent = 'Password must be at least 8 characters long.';
      el.authErrorAlert.classList.remove('hidden');
      return;
    }

    el.setupSubmitBtn.disabled = true;
    el.setupSubmitBtn.textContent = 'Initializing...';

    try {
      const res = await fetch('/admin/auth/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username,
          password,
          confirm_password: confirmPassword,
        }),
      });

      const body = await res.json();
      if (!res.ok) {
        throw new Error(body.error || 'Setup failed');
      }

      showToast('Admin account initialized successfully', 'success');
      state.currentUser = body.user;
      updateUserBadge(body.user);
      hideAuthScreen();
      startPolling();
      refreshAll();
    } catch (err) {
      el.authErrorAlert.textContent = err.message;
      el.authErrorAlert.classList.remove('hidden');
    } finally {
      el.setupSubmitBtn.disabled = false;
      el.setupSubmitBtn.textContent = 'Complete Setup & Secure Instance';
    }
  }

  async function handleLoginSubmit(e) {
    e.preventDefault();
    el.authErrorAlert.classList.add('hidden');

    const username = el.loginUsernameInput.value.trim();
    const password = el.loginPasswordInput.value;

    el.loginSubmitBtn.disabled = true;
    el.loginSubmitBtn.textContent = 'Signing in...';

    try {
      const res = await fetch('/admin/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });

      const body = await res.json();
      if (!res.ok) {
        throw new Error(body.error || 'Invalid credentials');
      }

      showToast(`Welcome back, ${body.user.username}!`, 'success');
      state.currentUser = body.user;
      updateUserBadge(body.user);
      hideAuthScreen();
      startPolling();
      refreshAll();
    } catch (err) {
      el.authErrorAlert.textContent = err.message;
      el.authErrorAlert.classList.remove('hidden');
    } finally {
      el.loginSubmitBtn.disabled = false;
      el.loginSubmitBtn.textContent = 'Sign In';
    }
  }

  async function handleSignOut() {
    try {
      await fetch('/admin/auth/logout', { method: 'POST' });
    } catch (_) {}
    state.currentUser = null;
    pausePolling();
    updateUserBadge(null);
    showAuthScreen('login');
    showToast('Signed out', 'info');
  }

  function openChangePasswordModal() {
    el.pwErrorAlert.classList.add('hidden');
    el.changePasswordForm.reset();
    el.changePasswordModal.classList.remove('hidden');
    el.pwOldInput.focus();
  }

  function closeChangePasswordModal() {
    el.changePasswordModal.classList.add('hidden');
  }

  async function handleChangePasswordSubmit(e) {
    e.preventDefault();
    el.pwErrorAlert.classList.add('hidden');

    const oldPassword = el.pwOldInput.value;
    const newPassword = el.pwNewInput.value;
    const confirmPassword = el.pwConfirmInput.value;

    if (newPassword !== confirmPassword) {
      el.pwErrorAlert.textContent = 'New passwords do not match.';
      el.pwErrorAlert.classList.remove('hidden');
      return;
    }
    if (newPassword.length < 8) {
      el.pwErrorAlert.textContent = 'New password must be at least 8 characters.';
      el.pwErrorAlert.classList.remove('hidden');
      return;
    }

    el.pwSaveBtn.disabled = true;
    try {
      const res = await apiFetch('/admin/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          old_password: oldPassword,
          new_password: newPassword,
          confirm_password: confirmPassword,
        }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error || 'Password update failed');
      showToast('Password updated successfully', 'success');
      closeChangePasswordModal();
    } catch (err) {
      el.pwErrorAlert.textContent = err.message;
      el.pwErrorAlert.classList.remove('hidden');
    } finally {
      el.pwSaveBtn.disabled = false;
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

    // MT Studio Listeners
    if (el.mtTextInput) {
      el.mtTextInput.addEventListener('input', updateMTCharCounter);
    }
    if (el.mtResetBtn) {
      el.mtResetBtn.addEventListener('click', () => {
        el.mtForm.reset();
        updateMTCharCounter();
      });
    }
    if (el.mtForm) {
      el.mtForm.addEventListener('submit', handleMTSubmit);
    }

    // Quick presets for chips
    document.querySelectorAll('.chip-btn').forEach((chip) => {
      chip.addEventListener('click', () => {
        const targetId = chip.getAttribute('data-target');
        const targetVal = chip.getAttribute('data-val');
        const targetEl = document.getElementById(targetId);
        if (targetEl) targetEl.value = targetVal;
      });
    });

    // Simulation tag chips
    document.querySelectorAll('.chip-badge[data-insert]').forEach((badge) => {
      badge.addEventListener('click', () => {
        const token = badge.getAttribute('data-insert');
        if (el.mtTextInput) {
          const current = el.mtTextInput.value;
          if (current.includes(token)) return;
          el.mtTextInput.value = token + ' ' + current;
          updateMTCharCounter();
        }
      });
    });

    // Virtual Receiver Listeners
    if (el.btnToggleVR) {
      el.btnToggleVR.addEventListener('click', handleToggleVR);
    }
    if (el.vrClearInboxBtn) {
      el.vrClearInboxBtn.addEventListener('click', handleClearVRInbox);
    }
    if (el.moOpSelect) {
      el.moOpSelect.addEventListener('change', () => {
        checkVRStatus(el.moOpSelect.value);
      });
    }

    // Copy Config
    el.btnCopyConfig.addEventListener('click', () => {
      if (!state.config) return;
      navigator.clipboard.writeText(JSON.stringify(state.config, null, 2)).then(() => {
        showToast('Configuration copied to clipboard', 'success');
      });
    });

    // Disconnect Modal
    el.modalCloseBtn.addEventListener('click', closeModal);
    el.modalCancelBtn.addEventListener('click', closeModal);
    el.disconnectModal.addEventListener('click', (e) => {
      if (e.target === el.disconnectModal) closeModal();
    });
    el.modalConfirmBtn.addEventListener('click', confirmDisconnect);

    // Operator Add/Edit Modal
    if (el.btnAddOperator) {
      el.btnAddOperator.addEventListener('click', openAddOperatorModal);
    }
    el.opModalCloseBtn.addEventListener('click', closeOperatorModal);
    el.opModalCancelBtn.addEventListener('click', closeOperatorModal);
    el.operatorModal.addEventListener('click', (e) => {
      if (e.target === el.operatorModal) closeOperatorModal();
    });
    el.btnAddAccount.addEventListener('click', () => addAccountRow());
    el.opRateLimitedCheckbox.addEventListener('change', (e) => {
      el.opThrottleFields.style.display = e.target.checked ? 'flex' : 'none';
    });
    el.opDlrEnabledCheckbox.addEventListener('change', (e) => {
      el.opDlrFields.style.display = e.target.checked ? 'flex' : 'none';
    });
    el.operatorForm.addEventListener('submit', handleOperatorSubmit);

    // Delete Operator Modal
    el.delOpModalCloseBtn.addEventListener('click', closeDeleteOpModal);
    el.delOpCancelBtn.addEventListener('click', closeDeleteOpModal);
    el.deleteOpModal.addEventListener('click', (e) => {
      if (e.target === el.deleteOpModal) closeDeleteOpModal();
    });
    el.delOpConfirmBtn.addEventListener('click', confirmDeleteOperator);

    // Auth listeners
    if (el.setupForm) el.setupForm.addEventListener('submit', handleSetupSubmit);
    if (el.loginForm) el.loginForm.addEventListener('submit', handleLoginSubmit);
    if (el.btnSignOut) el.btnSignOut.addEventListener('click', handleSignOut);
    if (el.btnChangePassword) el.btnChangePassword.addEventListener('click', openChangePasswordModal);
    if (el.pwModalCloseBtn) el.pwModalCloseBtn.addEventListener('click', closeChangePasswordModal);
    if (el.pwCancelBtn) el.pwCancelBtn.addEventListener('click', closeChangePasswordModal);
    if (el.changePasswordForm) el.changePasswordForm.addEventListener('submit', handleChangePasswordSubmit);

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
    const res = await apiFetch('/admin/overview');
    if (!res.ok) throw new Error('Overview HTTP ' + res.status);
    const data = await res.json();
    state.overview = data;
    renderKPIs();
    renderOperators();
    populateMOOperators();
    populateEventOperators();
  }

  async function fetchSessions() {
    const res = await apiFetch('/admin/sessions');
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
      const res = await apiFetch('/admin/config');
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
      if (ops.length === 0) {
        el.operatorsGrid.innerHTML = `
          <div class="empty-state card" style="grid-column: 1 / -1; padding: 56px 24px; text-align: center; border: 1px dashed rgba(255,255,255,0.15);">
            <div style="font-size: 3rem; margin-bottom: 14px;">🌐</div>
            <h3 style="font-size: 1.25rem; font-weight: 700; color: #fff; margin-bottom: 8px;">No SMSC Connections Configured</h3>
            <p style="color: var(--text-muted); max-width: 500px; margin: 0 auto 24px auto; line-height: 1.6;">
              This instance is ready for open-source global usage. Click <strong>+ Add Connection</strong> to create your first simulated operator endpoint (custom port, SMPP credentials, TPS rate limit, and latency).
            </p>
            <button class="btn btn-primary btn-lg" id="btn-empty-add-op" style="margin: 0 auto;">
              <svg class="icon-sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;">
                <line x1="12" y1="5" x2="12" y2="19"></line>
                <line x1="5" y1="12" x2="19" y2="12"></line>
              </svg>
              Add Connection
            </button>
          </div>
        `;
        const btn = document.getElementById('btn-empty-add-op');
        if (btn) btn.addEventListener('click', openAddOperatorModal);
        return;
      }

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
          <div class="op-card-actions">
            <button class="btn btn-secondary btn-xs btn-inject-mo" data-operator="${escapeHTML(op.name)}">
              MO
            </button>
            <button class="btn btn-secondary btn-xs btn-edit-op" data-operator="${escapeHTML(op.name)}">
              Edit
            </button>
            <button class="btn btn-danger btn-xs btn-delete-op" data-operator="${escapeHTML(op.name)}">
              Delete
            </button>
          </div>
        </div>
      `;

      card.querySelector('.btn-inject-mo').addEventListener('click', () => {
        openMOForOperator(op.name);
      });
      card.querySelector('.btn-edit-op').addEventListener('click', () => {
        openEditOperatorModal(op.name);
      });
      card.querySelector('.btn-delete-op').addEventListener('click', () => {
        openDeleteOpModal(op.name);
      });

      el.operatorsGrid.appendChild(card);
    });
  }

  // Operator CRUD UI Handlers
  function openAddOperatorModal() {
    if (el.opModalErrorAlert) {
      el.opModalErrorAlert.classList.add('hidden');
      el.opModalErrorAlert.textContent = '';
    }
    el.opFormIsEdit.value = 'false';
    el.opFormOldName.value = '';
    el.operatorModalTitle.textContent = 'Add New Connection';
    el.operatorForm.reset();

    el.opAccountsContainer.innerHTML = '';
    addAccountRow('', '');

    el.opRateLimitedCheckbox.checked = true;
    el.opThrottleFields.style.display = 'flex';
    el.opDlrEnabledCheckbox.checked = true;
    el.opDlrFields.style.display = 'flex';
    if (el.opFaultStatusSelect) el.opFaultStatusSelect.value = '0';
    if (el.opFaultErrorPctInput) el.opFaultErrorPctInput.value = '0';
    if (el.opFaultBindPctInput) el.opFaultBindPctInput.value = '0';
    if (el.opFaultNackPctInput) el.opFaultNackPctInput.value = '0';

    el.operatorModal.classList.remove('hidden');
    el.opNameInput.focus();
  }

  async function openEditOperatorModal(opName) {
    if (el.opModalErrorAlert) {
      el.opModalErrorAlert.classList.add('hidden');
      el.opModalErrorAlert.textContent = '';
    }
    el.opFormIsEdit.value = 'true';
    el.opFormOldName.value = opName;
    el.operatorModalTitle.textContent = `Edit Connection: ${opName}`;

    try {
      const res = await apiFetch(`/admin/operators/${encodeURIComponent(opName)}`);
      if (!res.ok) throw new Error('Failed to fetch operator config');
      const op = await res.json();

      el.opNameInput.value = op.name || opName;
      el.opListenInput.value = op.listen || '';
      el.opVersionSelect.value = op.smpp_version || '3.4';
      el.opWindowInput.value = op.window_size || 10;

      el.opAccountsContainer.innerHTML = '';
      if (op.accounts && op.accounts.length > 0) {
        op.accounts.forEach((a) => addAccountRow(a.system_id, a.password));
      } else {
        addAccountRow('', '');
      }

      const hasThrottle = op.throttle && (op.throttle.tps > 0 || op.throttle.count > 0);
      el.opRateLimitedCheckbox.checked = hasThrottle;
      el.opThrottleFields.style.display = hasThrottle ? 'flex' : 'none';
      if (hasThrottle) {
        el.opTpsInput.value = op.throttle.tps || 100;
        el.opBurstInput.value = op.throttle.burst || 100;
      }

      const dlrOn = op.dlr && (op.dlr.enabled === undefined || op.dlr.enabled === true);
      el.opDlrEnabledCheckbox.checked = dlrOn;
      el.opDlrFields.style.display = dlrOn ? 'flex' : 'none';
      if (op.dlr && op.dlr.delay) {
        el.opDlrMinInput.value = op.dlr.delay.min || '2s';
        el.opDlrMaxInput.value = op.dlr.delay.max || '15s';
      }

      if (el.opFaultStatusSelect) {
        el.opFaultStatusSelect.value = (op.faults && op.faults.submit_status !== undefined) ? op.faults.submit_status : '0';
      }
      if (el.opFaultErrorPctInput) {
        el.opFaultErrorPctInput.value = (op.faults && op.faults.submit_error_pct !== undefined) ? op.faults.submit_error_pct : '0';
      }
      if (el.opFaultBindPctInput) {
        el.opFaultBindPctInput.value = (op.faults && op.faults.reject_bind_pct !== undefined) ? op.faults.reject_bind_pct : '0';
      }
      if (el.opFaultNackPctInput) {
        el.opFaultNackPctInput.value = (op.faults && op.faults.generic_nack_pct !== undefined) ? op.faults.generic_nack_pct : '0';
      }

      el.operatorModal.classList.remove('hidden');
    } catch (err) {
      showToast('Error loading operator: ' + err.message, 'error');
    }
  }

  function closeOperatorModal() {
    el.operatorModal.classList.add('hidden');
  }

  function addAccountRow(systemId = '', password = '') {
    const row = document.createElement('div');
    row.className = 'account-row';
    row.innerHTML = `
      <input type="text" class="form-input acc-sysid" placeholder="system_id" value="${escapeHTML(systemId)}" required>
      <input type="text" class="form-input acc-pass" placeholder="password" value="${escapeHTML(password)}" required>
      <button type="button" class="btn btn-ghost btn-xs btn-remove-acc" title="Remove account">&times;</button>
    `;
    row.querySelector('.btn-remove-acc').addEventListener('click', () => {
      if (el.opAccountsContainer.children.length > 1) {
        row.remove();
      } else {
        showToast('At least one account is required', 'warn');
      }
    });
    el.opAccountsContainer.appendChild(row);
  }

  async function handleOperatorSubmit(e) {
    e.preventDefault();
    if (el.opModalErrorAlert) {
      el.opModalErrorAlert.classList.add('hidden');
      el.opModalErrorAlert.textContent = '';
    }

    const isEdit = el.opFormIsEdit.value === 'true';
    const oldName = el.opFormOldName.value;

    const name = el.opNameInput.value.trim();
    let listen = el.opListenInput.value.trim();
    if (listen && !listen.includes(':')) {
      listen = ':' + listen;
      el.opListenInput.value = listen;
    }
    const smppVersion = el.opVersionSelect.value;
    const windowSize = parseInt(el.opWindowInput.value, 10) || 10;

    const accounts = [];
    const rows = el.opAccountsContainer.querySelectorAll('.account-row');
    rows.forEach((r) => {
      const sysId = r.querySelector('.acc-sysid').value.trim();
      const pass = r.querySelector('.acc-pass').value.trim();
      if (sysId) {
        accounts.push({ system_id: sysId, password: pass });
      }
    });

    if (accounts.length === 0) {
      const errMsg = 'At least one account (system_id & password) is required';
      if (el.opModalErrorAlert) {
        el.opModalErrorAlert.textContent = errMsg;
        el.opModalErrorAlert.classList.remove('hidden');
      }
      showToast(errMsg, 'error');
      return;
    }

    const payload = {
      name: name,
      listen: listen,
      smpp_version: smppVersion,
      window_size: windowSize,
      accounts: accounts,
      bind_types: ['tx', 'rx', 'trx'],
      throttle: {},
      dlr: {
        enabled: el.opDlrEnabledCheckbox.checked,
        delay: {
          min: el.opDlrMinInput.value.trim() || '2s',
          max: el.opDlrMaxInput.value.trim() || '15s',
        },
      },
      faults: {
        submit_status: el.opFaultStatusSelect ? (parseInt(el.opFaultStatusSelect.value, 10) || 0) : 0,
        submit_error_pct: el.opFaultErrorPctInput ? (parseFloat(el.opFaultErrorPctInput.value) || 0) : 0,
        reject_bind_pct: el.opFaultBindPctInput ? (parseFloat(el.opFaultBindPctInput.value) || 0) : 0,
        generic_nack_pct: el.opFaultNackPctInput ? (parseFloat(el.opFaultNackPctInput.value) || 0) : 0,
        drop_after: '0s',
      },
    };

    if (el.opRateLimitedCheckbox.checked) {
      payload.throttle.tps = parseFloat(el.opTpsInput.value) || 100;
      payload.throttle.burst = parseFloat(el.opBurstInput.value) || 100;
    }

    el.opModalSaveBtn.disabled = true;

    try {
      const url = isEdit ? `/admin/operators/${encodeURIComponent(oldName)}` : '/admin/operators';
      const method = isEdit ? 'PUT' : 'POST';

      const res = await apiFetch(url, {
        method: method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      const body = await res.json();
      if (!res.ok) {
        throw new Error(body.error || `HTTP ${res.status}`);
      }

      showToast(`Connection ${name} ${isEdit ? 'updated' : 'created'} successfully!`, 'success');
      closeOperatorModal();
      await fetchOverview();
      await fetchEvents();
    } catch (err) {
      if (el.opModalErrorAlert) {
        el.opModalErrorAlert.textContent = err.message;
        el.opModalErrorAlert.classList.remove('hidden');
        const scrollBody = el.operatorModal.querySelector('.modal-scroll');
        if (scrollBody) scrollBody.scrollTop = 0;
      }
      showToast(err.message, 'error');
    } finally {
      el.opModalSaveBtn.disabled = false;
    }
  }

  function openDeleteOpModal(opName) {
    if (el.delOpModalErrorAlert) {
      el.delOpModalErrorAlert.classList.add('hidden');
      el.delOpModalErrorAlert.textContent = '';
    }
    state.targetDeleteOpName = opName;
    el.delOpModalName.textContent = opName;
    el.deleteOpModal.classList.remove('hidden');
  }

  function closeDeleteOpModal() {
    state.targetDeleteOpName = null;
    el.deleteOpModal.classList.add('hidden');
  }

  async function confirmDeleteOperator() {
    const name = state.targetDeleteOpName;
    if (!name) return;

    el.delOpConfirmBtn.disabled = true;
    try {
      const res = await apiFetch(`/admin/operators/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      });
      const body = await res.json();
      if (!res.ok) {
        throw new Error(body.error || `HTTP ${res.status}`);
      }

      showToast(`Operator ${name} deleted`, 'warn');
      closeDeleteOpModal();
      await fetchOverview();
      await fetchEvents();
    } catch (err) {
      if (el.delOpModalErrorAlert) {
        el.delOpModalErrorAlert.textContent = err.message;
        el.delOpModalErrorAlert.classList.remove('hidden');
      }
      showToast('Delete failed: ' + err.message, 'error');
    } finally {
      el.delOpConfirmBtn.disabled = false;
    }
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

    if (el.mtOpSelect) {
      const currentMTVal = el.mtOpSelect.value;
      el.mtOpSelect.innerHTML = '<option value="" disabled>Select an operator...</option>';
      state.overview.operators.forEach((op) => {
        const opt = document.createElement('option');
        opt.value = op.name;
        opt.textContent = `${op.name} (${op.listen})`;
        el.mtOpSelect.appendChild(opt);
      });
      if (currentMTVal && state.overview.operators.some((o) => o.name === currentMTVal)) {
        el.mtOpSelect.value = currentMTVal;
      } else if (state.overview.operators.length > 0) {
        el.mtOpSelect.value = state.overview.operators[0].name;
      }
    }

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
      const res = await apiFetch(`/admin/sessions/${encodeURIComponent(id)}/disconnect`, {
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
