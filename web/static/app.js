// WebSocket 连接与自动重连
let ws;
let reconnectTimer;

const EVENT_TYPE_ZH = {
    state_change: '状态变更',
    leader_changed: '主机切换',
    circuit_open: '熔断触发',
    circuit_close: '熔断恢复',
    ddns_update: 'DDNS 更新',
    ddns_match: 'DDNS 校验',
    ddns_mismatch: 'DDNS 异常',
    exec: '执行结果',
    test: '测试通知',
};

function eventTypeZh(type) {
    return EVENT_TYPE_ZH[type] || type || '事件';
}

function stateZh(s) {
    const m = { ready: '可用', full: '满载', dead: '不可用' };
    return m[s] || s || '-';
}

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
    const reachable = traffic && typeof traffic.reachable !== 'undefined'
        ? !!traffic.reachable
        : meta.getAttribute('data-reachable') === 'true';
    const inBytes = traffic && typeof traffic.in !== 'undefined'
        ? Number(traffic.in) || 0
        : Number(meta.getAttribute('data-traffic-in')) || 0;
    const outBytes = traffic && typeof traffic.out !== 'undefined'
        ? Number(traffic.out) || 0
        : Number(meta.getAttribute('data-traffic-out')) || 0;

    const sshReachable = traffic && typeof traffic.ssh_reachable !== 'undefined'
        ? !!traffic.ssh_reachable
        : meta.getAttribute('data-ssh-reachable') === 'true';
    const sshError = traffic && typeof traffic.ssh_error !== 'undefined'
        ? String(traffic.ssh_error || '')
        : String(meta.getAttribute('data-ssh-error') || '');
    const netIface = traffic && typeof traffic.net_iface !== 'undefined'
        ? String(traffic.net_iface || '')
        : String(meta.getAttribute('data-net-iface') || '');

    // Persist latest values for future incremental updates
    meta.setAttribute('data-traffic-threshold', String(threshold));
    meta.setAttribute('data-reachable', String(reachable));
    meta.setAttribute('data-traffic-in', String(inBytes));
    meta.setAttribute('data-traffic-out', String(outBytes));
    meta.setAttribute('data-ssh-reachable', String(sshReachable));
    meta.setAttribute('data-ssh-error', sshError);
    meta.setAttribute('data-net-iface', netIface);

    const onlineEl = document.getElementById('online-status-' + hostId);
    const inEl = document.getElementById('traffic-in-' + hostId);
    const outEl = document.getElementById('traffic-out-' + hostId);
    const usedEl = document.getElementById('traffic-used-' + hostId);
    const thrEl = document.getElementById('traffic-threshold-' + hostId);
    const sshEl = document.getElementById('ssh-status-' + hostId);
    const ifaceEl = document.getElementById('traffic-iface-' + hostId);

    if (onlineEl) {
        onlineEl.textContent = reachable ? '在线' : '离线';
        onlineEl.style.color = reachable ? '#4ade80' : '#f87171';
    }
    if (inEl) inEl.textContent = fmtBytes(inBytes);
    if (outEl) outEl.textContent = fmtBytes(outBytes);
    if (usedEl) usedEl.textContent = fmtBytes(Math.max(inBytes, outBytes));
    if (thrEl) thrEl.textContent = fmtBytes(threshold);

    if (sshEl) {
        sshEl.textContent = sshReachable ? '可连接' : `失败${sshError ? ` (${sshError})` : ''}`;
        sshEl.style.color = sshReachable ? '#4ade80' : '#f87171';
    }
    if (ifaceEl) {
        ifaceEl.textContent = netIface || '-';
    }

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

    const noHosts = banner.getAttribute('data-no-hosts') === 'true';
    const titleEl = document.getElementById('circuit-banner-title');
    const descEl = document.getElementById('circuit-banner-desc');
    const iconEl = document.getElementById('circuit-banner-icon');

    if (noHosts) {
        banner.classList.add('active', 'warning');
        if (titleEl) titleEl.textContent = '请添加主机';
        if (descEl) descEl.textContent = '主机池为空，请前往设置页添加主机';
        if (iconEl) iconEl.textContent = '⚠️';
        return;
    }

    banner.classList.remove('warning');
    if (titleEl) titleEl.textContent = '熔断器已触发';
    if (descEl) descEl.textContent = '所有主机均不可用，请立即介入';
    if (iconEl) iconEl.textContent = '⚠';

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
        <span style="margin-left:8px;color:rgba(99,102,241,0.9);">[${eventTypeZh('state_change')}]</span>
        <span style="margin-left:8px;">主机 ${data.host_id}: ${stateZh(data.old_state)} → ${stateZh(data.new_state)}</span>`;
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
