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

function applyBrandConfig() {
    const body = document.body;
    if (!body) return;

    const siteTitle = (body.getAttribute('data-site-title') || '').trim();
    const bgUrl = (body.getAttribute('data-background-image-url') || '').trim();

    if (siteTitle) {
        document.title = siteTitle;
    }

    if (bgUrl) {
        body.classList.add('has-custom-bg');
        body.style.backgroundImage = `linear-gradient(rgba(255,255,255,0.86), rgba(255,255,255,0.86)), url('${bgUrl.replace(/'/g, "\\'")}')`;
    } else {
        body.classList.remove('has-custom-bg');
        body.style.removeProperty('background-image');
    }
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

function inferEventTypeFromItem(item) {
    const explicit = item.getAttribute('data-event-type');
    if (explicit) return explicit;

    const badgeText = (item.querySelector('.event-type-badge')?.textContent || '').trim();
    const reverse = {
        '状态变更': 'state_change',
        '主机切换': 'leader_changed',
        '熔断触发': 'circuit_open',
        '熔断恢复': 'circuit_close',
        'DDNS 更新': 'ddns_update',
        'DDNS 校验': 'ddns_match',
        'DDNS 异常': 'ddns_mismatch',
        '执行结果': 'exec',
        '测试通知': 'test',
    };
    return reverse[badgeText] || 'other';
}

function resolveHostDisplayFromText(details) {
    const byNameIp = details.match(/([\w.-]+)\s*\(([^)]+)\)/);
    if (byNameIp) {
        const maybeName = String(byNameIp[1] || '').trim();
        const maybeIP = String(byNameIp[2] || '').trim();
        const looksLikeIP = /^(?:\d{1,3}\.){3}\d{1,3}$/.test(maybeIP) || /:/.test(maybeIP);
        const looksLikeNumericId = /^\d+$/.test(maybeName);
        if (looksLikeIP && !looksLikeNumericId) {
            return { hostName: maybeName, hostIP: maybeIP };
        }
    }

    const hostIdPattern = details.match(/(?:host|主机)\s*[:：]?\s*(\d+)/i) || details.match(/\b(\d+)\s*(?:\(|:)/);
    if (hostIdPattern) {
        const id = hostIdPattern[1];
        const card = document.getElementById('host-card-' + id);
        if (card) {
            const hostName = card.getAttribute('data-host-name') || '';
            const hostIP = card.getAttribute('data-host-ip') || '';
            if (hostName || hostIP) {
                return { hostName, hostIP, hostId: id };
            }
        }
        return { hostId: id };
    }

    return {};
}

function parseEventByType(type, raw) {
    const details = String(raw || '').replace(/\\n/g, '\n').trim();
    if (!details) return { summary: '-', tags: [], status: 'neutral', details: '' };

    const ipMatch = details.match(/\b(?:\d{1,3}\.){3}\d{1,3}\b/);
    const hostMatch = details.match(/(?:host|主机)\s*[:：]?\s*([\w.-]+)/i);
    const hostRef = resolveHostDisplayFromText(details);
    const oldNewMatch = details.match(/(ready|full|dead|可用|满载|不可用)\s*(?:->|→)\s*(ready|full|dead|可用|满载|不可用)/i);
    const exitMatch = details.match(/exit\s*=?\s*([0-9]+)/i);
    const durationMatch = details.match(/duration\s*=?\s*([0-9a-zA-Z:.]+)/i);

    let summary = details.split('\n')[0].replace(/\s+/g, ' ').trim();
    let tags = [];
    let status = 'neutral';

    const hostDisplay = hostRef.hostName && hostRef.hostIP
        ? `${hostRef.hostName} (${hostRef.hostIP})`
        : (hostRef.hostName || (hostRef.hostId ? `主机ID ${hostRef.hostId}` : (hostMatch?.[1] && !/^\d+$/.test(hostMatch[1]) ? hostMatch[1] : '')));
    const resolvedIP = hostRef.hostIP || ipMatch?.[0] || '';

    if (type === 'state_change') {
        summary = oldNewMatch ? `状态变更：${oldNewMatch[1]} → ${oldNewMatch[2]}` : summary;
        if (hostDisplay) tags.push({ label: '主机', value: hostDisplay });
        if (resolvedIP) tags.push({ label: 'IP', value: resolvedIP });
        if (oldNewMatch) tags.push({ label: '变化', value: `${oldNewMatch[1]} → ${oldNewMatch[2]}` });
        status = /dead|不可用|失败|error/i.test(details) ? 'failed' : 'success';
    } else if (type.startsWith('ddns')) {
        summary = /mismatch|异常|failed|error/i.test(details) ? 'DDNS 异常，目标记录未正确对齐' : 'DDNS 校验/更新完成';
        if (hostDisplay) tags.push({ label: '主机', value: hostDisplay });
        if (resolvedIP) tags.push({ label: 'IP', value: resolvedIP });
        const domainMatch = details.match(/([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})/);
        if (domainMatch) tags.push({ label: '域名', value: domainMatch[1] });
        status = /mismatch|异常|failed|error|timeout/i.test(details) ? 'failed' : 'success';
    } else if (type === 'exec') {
        summary = /failed|error|timeout|refused|denied|not found|失败/i.test(details)
            ? '命令执行失败'
            : '命令执行完成';
        if (hostDisplay) tags.push({ label: '主机', value: hostDisplay });
        if (exitMatch) tags.push({ label: '退出码', value: exitMatch[1] });
        if (durationMatch) tags.push({ label: '耗时', value: durationMatch[1] });
        if (resolvedIP) tags.push({ label: 'IP', value: resolvedIP });
        status = exitMatch ? (exitMatch[1] === '0' ? 'success' : 'failed') : (/failed|error|失败|timeout/i.test(details) ? 'failed' : 'success');
    } else {
        if (hostDisplay) tags.push({ label: '主机', value: hostDisplay });
        if (resolvedIP) tags.push({ label: 'IP', value: resolvedIP });
        status = /failed|error|失败|异常|timeout/i.test(details) ? 'failed' : (/success|ok|成功/i.test(details) ? 'success' : 'neutral');
    }

    if (summary.length > 140) summary = `${summary.slice(0, 140)}...`;
    return { summary, tags: tags.slice(0, 4), status, details };
}

function enhanceEventItem(item) {
    if (!item || item.getAttribute('data-enhanced') === 'true') return;
    const messageEl = item.querySelector('.event-message');
    if (!messageEl) return;

    const eventType = inferEventTypeFromItem(item);
    const rawText = messageEl.textContent || '';
    const { summary, details, tags, status } = parseEventByType(eventType, rawText);
    const isLong = details.length > 180 || details.includes('\n');

    item.classList.remove('event-success', 'event-failed');
    if (status === 'success') item.classList.add('event-success');
    if (status === 'failed') item.classList.add('event-failed');

    messageEl.innerHTML = '';

    const summaryEl = document.createElement('div');
    summaryEl.className = 'event-summary';
    summaryEl.textContent = summary;
    messageEl.appendChild(summaryEl);

    if (tags.length > 0) {
        const metaWrap = document.createElement('div');
        metaWrap.className = 'event-meta-cards';
        tags.forEach(tag => {
            const card = document.createElement('div');
            card.className = 'event-meta-card';

            const label = document.createElement('div');
            label.className = 'event-meta-label';
            label.textContent = tag.label;

            const value = document.createElement('div');
            value.className = 'event-meta-value';
            value.textContent = tag.value;

            card.appendChild(label);
            card.appendChild(value);
            metaWrap.appendChild(card);
        });
        messageEl.appendChild(metaWrap);
    }

    if (isLong) {
        const detailsEl = document.createElement('details');
        detailsEl.className = 'event-details';

        const summaryToggle = document.createElement('summary');
        summaryToggle.textContent = '查看详情';

        const pre = document.createElement('pre');
        pre.className = 'event-details-pre';
        pre.textContent = details;

        detailsEl.appendChild(summaryToggle);
        detailsEl.appendChild(pre);
        messageEl.appendChild(detailsEl);
    }

    item.setAttribute('data-enhanced', 'true');
}

function makeEventItemCollapsible(item, openByDefault = false) {
    if (!item || item.getAttribute('data-collapsible') === 'true') return;

    const head = item.querySelector('.event-item-head');
    const messageEl = item.querySelector('.event-message');
    if (!head || !messageEl) return;

    const details = document.createElement('details');
    details.className = 'event-collapse';
    details.open = !!openByDefault;

    const summary = document.createElement('summary');
    summary.className = 'event-collapse-summary';
    summary.appendChild(head);

    const body = document.createElement('div');
    body.className = 'event-collapse-body';
    body.appendChild(messageEl);

    details.appendChild(summary);
    details.appendChild(body);

    item.innerHTML = '';
    item.appendChild(details);
    item.setAttribute('data-collapsible', 'true');
}

function refreshRecentEventsLatest() {
    const latestBox = document.getElementById('recent-events-latest');
    if (!latestBox) return;

    const first = document.querySelector('#event-list .event-item');
    const timeEl = document.getElementById('recent-card-latest-time');
    const typeEl = document.getElementById('recent-card-latest-type');
    if (!timeEl || !typeEl) return;

    if (!first) {
        timeEl.textContent = '暂无事件';
        typeEl.textContent = '-';
        return;
    }

    const firstTime = first.querySelector('.event-item-head .event-time')?.textContent?.trim() || '暂无事件';
    const firstType = first.querySelector('.event-item-head .event-type-badge')?.textContent?.trim() || '-';
    timeEl.textContent = firstTime;
    typeEl.textContent = firstType;
}

function enhanceEventList() {
    const list = document.getElementById('event-list');
    if (!list) return;
    const items = list.querySelectorAll('.event-item');
    items.forEach((item, idx) => {
        enhanceEventItem(item);
        makeEventItemCollapsible(item, idx === 0);
    });
    refreshRecentEventsLatest();
}

// 在事件流顶部插入新事件
function prependEvent(data) {
    const list = document.getElementById('event-list');
    if (!list) return;
    const item = document.createElement('div');
    item.className = 'event-item';
    item.setAttribute('data-event-type', 'state_change');

    const head = document.createElement('div');
    head.className = 'event-item-head';

    const timeEl = document.createElement('span');
    timeEl.className = 'event-time';
    timeEl.textContent = data.time ? new Date(data.time).toLocaleString('zh-CN') : new Date().toLocaleString('zh-CN');

    const badgeEl = document.createElement('span');
    badgeEl.className = 'event-type-badge';
    badgeEl.textContent = eventTypeZh('state_change');

    head.appendChild(timeEl);
    head.appendChild(badgeEl);

    const msg = document.createElement('div');
    msg.className = 'event-message';
    msg.textContent = `主机 ${data.host_id}: ${stateZh(data.old_state)} → ${stateZh(data.new_state)}`;

    item.appendChild(head);
    item.appendChild(msg);

    enhanceEventItem(item);
    makeEventItemCollapsible(item, true);
    list.insertBefore(item, list.firstChild);

    const all = list.querySelectorAll('.event-item .event-collapse');
    all.forEach((d, i) => {
        d.open = i === 0;
    });

    // Keep at most 20 items
    while (list.children.length > 20) {
        list.removeChild(list.lastChild);
    }

    refreshRecentEventsLatest();
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

function togglePoolCarrierPanel() {
    const panel = document.getElementById('pool-carrier-panel');
    const btn = document.getElementById('pool-carrier-toggle');
    if (!panel || !btn) return;
    panel.classList.toggle('collapsed');
    const collapsed = panel.classList.contains('collapsed');
    btn.textContent = collapsed ? '展开更多' : '收起';
}

function initMobileSidebar() {
    const menuToggle = document.querySelector('.mobile-menu-toggle');
    const backdrop = document.getElementById('sidebar-backdrop');
    if (!menuToggle || !backdrop) return;

    const close = () => {
        document.body.classList.remove('sidebar-open');
        backdrop.classList.remove('active');
    };

    menuToggle.addEventListener('click', () => {
        const open = document.body.classList.toggle('sidebar-open');
        backdrop.classList.toggle('active', open);
    });

    backdrop.addEventListener('click', close);

    document.querySelectorAll('.side-menu-link').forEach((el) => {
        el.addEventListener('click', () => {
            if (window.innerWidth <= 980) close();
        });
    });
}

applyBrandConfig();
initThemeToggle();
initMobileSidebar();
enhanceEventList();

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
