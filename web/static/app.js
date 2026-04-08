// WebSocket 连接与自动重连
let ws;
let reconnectTimer;

function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${location.host}/ws`);

    ws.onmessage = (event) => {
        try {
            const msg = JSON.parse(event.data);
            if (msg.type === 'snapshot') handleSnapshot(msg.data);
            else if (msg.type === 'state_change') handleStateChange(msg.data);
            else if (msg.type === 'poll_summary') handlePollSummary(msg.data);
        } catch (e) {
            console.warn('ws parse error', e);
        }
    };

    ws.onclose = () => {
        clearTimeout(reconnectTimer);
        reconnectTimer = setTimeout(connect, 3000);
    };

    ws.onerror = () => ws.close();
}

// 处理完整快照
function handleSnapshot(data) {
    if (!data) return;
    updateCircuitBanner(data.circuit_open);
    if (Array.isArray(data.hosts)) {
        data.hosts.forEach(h => {
            updateHostCard(h.id, h.state);
            const t = data.traffic && data.traffic[h.id] ? data.traffic[h.id] : null;
            updateHostTraffic(h.id, h.traffic_threshold, t);
        });
    }
    if (data.leader_id) {
        const leaderEl = document.getElementById('leader-name');
        // leader name update requires full host data; skip if not available
    }
}

// 处理单个状态变更
function handleStateChange(data) {
    if (!data) return;
    updateHostCard(data.host_id, data.new_state);
    prependEvent(data);
}

// 处理轮询摘要
function handlePollSummary(data) {
    if (!data) return;
    updateCircuitBanner(data.circuit_open);
    if (Array.isArray(data.hosts)) {
        data.hosts.forEach(h => {
            updateHostCard(h.id, h.state);
            const t = data.traffic && data.traffic[h.id] ? data.traffic[h.id] : null;
            updateHostTraffic(h.id, h.traffic_threshold, t);
        });
    }
}

// 更新主机卡片状态徽章（带动画）
function updateHostCard(hostId, newState) {
    const badge = document.getElementById('badge-' + hostId);
    if (!badge) return;

    const oldState = badge.textContent.trim();
    if (oldState === newState) return;

    badge.textContent = newState;
    badge.className = 'badge badge-' + newState;
    badge.classList.add('state-changed');
    setTimeout(() => badge.classList.remove('state-changed'), 600);

    // Update progress bar color if present
    const progress = document.getElementById('progress-' + hostId);
    if (progress) {
        progress.className = 'progress-fill ' + newState;
    }
}

function fmtBytes(bytes) {
    const n = Number(bytes || 0);
    if (!Number.isFinite(n) || n < 0) return '-';
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    let v = n;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    const fixed = i === 0 ? 0 : (v >= 100 ? 0 : (v >= 10 ? 1 : 2));
    return `${v.toFixed(fixed)} ${units[i]}`;
}

function updateHostTraffic(hostId, trafficThreshold, traffic) {
    const meta = document.getElementById('traffic-meta-' + hostId);
    if (!meta) return;

    const threshold = Number(trafficThreshold ?? meta.getAttribute('data-traffic-threshold') ?? 0) || 0;
    const inBytes = traffic && typeof traffic.in !== 'undefined'
        ? Number(traffic.in) || 0
        : Number(meta.getAttribute('data-traffic-in')) || 0;
    const outBytes = traffic && typeof traffic.out !== 'undefined'
        ? Number(traffic.out) || 0
        : Number(meta.getAttribute('data-traffic-out')) || 0;

    // Persist latest values for future incremental updates
    meta.setAttribute('data-traffic-threshold', String(threshold));
    meta.setAttribute('data-traffic-in', String(inBytes));
    meta.setAttribute('data-traffic-out', String(outBytes));

    const inEl = document.getElementById('traffic-in-' + hostId);
    const outEl = document.getElementById('traffic-out-' + hostId);
    const usedEl = document.getElementById('traffic-used-' + hostId);
    const thrEl = document.getElementById('traffic-threshold-' + hostId);

    if (inEl) inEl.textContent = fmtBytes(inBytes);
    if (outEl) outEl.textContent = fmtBytes(outBytes);
    if (usedEl) usedEl.textContent = fmtBytes(Math.max(inBytes, outBytes));
    if (thrEl) thrEl.textContent = fmtBytes(threshold);

    const progress = document.getElementById('progress-' + hostId);
    if (progress && threshold > 0) {
        const pct = Math.max(0, Math.min(100, (Math.max(inBytes, outBytes) / threshold) * 100));
        progress.style.width = pct.toFixed(1) + '%';
    }
}

// 更新熔断横幅
function updateCircuitBanner(open) {
    const banner = document.getElementById('circuit-banner');
    if (!banner) return;
    if (open) {
        banner.classList.add('active');
    } else {
        banner.classList.remove('active');
    }
}

// 在事件流顶部插入新事件
function prependEvent(data) {
    const list = document.getElementById('event-list');
    if (!list) return;
    const item = document.createElement('div');
    item.className = 'event-item';
    const time = data.time ? new Date(data.time).toLocaleString('zh-CN') : new Date().toLocaleString('zh-CN');
    item.innerHTML = `<span class="event-time">${time}</span>
        <span style="margin-left:8px;color:rgba(99,102,241,0.9);">[state_change]</span>
        <span style="margin-left:8px;">host ${data.host_id}: ${data.old_state} → ${data.new_state}</span>`;
    list.insertBefore(item, list.firstChild);
    // Keep at most 20 items
    while (list.children.length > 20) {
        list.removeChild(list.lastChild);
    }
}

function applyTheme(theme) {
    const isLight = theme === 'light';
    document.body.classList.toggle('theme-light', isLight);
    const toggle = document.getElementById('theme-toggle');
    if (toggle) {
        toggle.textContent = isLight ? '☀️' : '🌙';
        toggle.setAttribute('aria-label', isLight ? '切换到深色主题' : '切换到浅色主题');
        toggle.title = isLight ? '浅色主题' : '深色主题';
    }
}

function initThemeToggle() {
    const toggle = document.getElementById('theme-toggle');
    if (!toggle) return;

    const saved = localStorage.getItem('ui-theme');
    const preferred = saved || 'dark';
    applyTheme(preferred);

    toggle.addEventListener('click', () => {
        const next = document.body.classList.contains('theme-light') ? 'dark' : 'light';
        localStorage.setItem('ui-theme', next);
        applyTheme(next);
    });
}

initThemeToggle();

// Only connect on pages that have the event list or host grid (i.e., index page)
if (document.getElementById('host-grid') || document.getElementById('event-list')) {
    // Initialize from server-rendered data-* attributes (no WS needed)
    const metas = document.querySelectorAll('[id^="traffic-meta-"]');
    metas.forEach(el => {
        const id = el.id.replace('traffic-meta-', '');
        const hostId = Number(id);
        if (!Number.isFinite(hostId)) return;
        updateHostTraffic(hostId, null, null);
    });
    connect();
}
