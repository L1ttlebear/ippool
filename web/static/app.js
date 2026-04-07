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
        data.hosts.forEach(h => updateHostCard(h.id, h.state));
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
        data.hosts.forEach(h => updateHostCard(h.id, h.state));
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

// Only connect on pages that have the event list or host grid (i.e., index page)
if (document.getElementById('host-grid') || document.getElementById('event-list')) {
    connect();
}
