let currentConfig = { services: [], client_keys: [] };

// --- Navigation ---
function switchPage(pageId) {
    document.querySelectorAll('.page').forEach(el => el.classList.remove('active'));
    document.getElementById(pageId).classList.add('active');
    document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
    event.target.classList.add('active');

    if (pageId === 'stats') {
        // Init date if empty
        if (!document.getElementById('stats_date').value) {
            document.getElementById('stats_date').valueAsDate = new Date();
        }
        loadStats();
    }
}


// --- Authentication ---
const token = localStorage.getItem('token');
const user = JSON.parse(localStorage.getItem('user') || '{}');

// Only called at startup
async function checkAuth() {
    if (!token) {
        window.location.href = 'login.html';
        return;
    }

    // Show Admin Panel Link if admin
    if (user.role === 'admin' || user.role === 'super_admin') {
        const btnAdmin = document.getElementById('btn-admin-panel');
        if (btnAdmin) btnAdmin.style.display = 'inline-flex';
    }

    // Reveal Content
    document.getElementById('app-content').style.display = 'block';

    // Load Data
    try {
        await Promise.all([renderServices(), renderKeys(), updatePlaygroundDropdowns()]);
    } catch (e) {
        // invalid token?
    }
}

// --- Initialization ---
window.onload = checkAuth;

function logout() {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    window.location.href = 'login.html';
}

// --- Rendering Services ---
function renderServices() {
    const grid = document.getElementById('service_grid');
    grid.innerHTML = '';

    currentConfig.services.forEach(service => {
        const card = document.createElement('div');
        card.className = `service-card`;
        
        const typeIcon = getIconForType(service.type);
        
        card.innerHTML = `
            <div class="service-header">
                <div class="service-icon">${typeIcon}</div>
                <div>
                    <div class="service-name">${service.name}</div>
                    <div class="service-type">${service.type}</div>
                </div>
            </div>
            <div class="service-details">
                <div class="detail-row">
                    <span>Base URL</span>
                    <span title="${service.base_url || 'Default'}" style="max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                        ${service.base_url || 'Default'}
                    </span>
                </div>
                <div class="detail-row">
                    <span>Model ID</span>
                    <span style="color: var(--primary); font-family: monospace;">${service.name}</span>
                </div>
            </div>

        `;
        grid.appendChild(card);
    });
}

function getIconForType(type) {
    switch(type) {
        case 'openai': return '🤖';
        case 'gemini': return '💎';
        case 'anthropic': return '🧠';
        default: return '🔌';
    }
}

// --- CRUD Services ---
async function saveAllServices() {
    try {
        const res = await fetch('/api/services', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify(currentConfig.services)
        });
        if (res.ok) {
            renderServices();
            updatePlaygroundDropdowns();
        } else {
            alert('Failed to save changes');
        }
    } catch (e) { console.error(e); }
}

function deleteService(id) {
    if (!confirm('Are you sure?')) return;
    currentConfig.services = currentConfig.services.filter(s => s.id !== id);
    saveAllServices();
}

// --- CRUD Keys ---
async function saveKeys() {
    try {
        const res = await fetch('/api/keys', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify(currentConfig.client_keys)
        });
        if (res.ok) {
            renderKeys();
            updatePlaygroundDropdowns();
        } 
    } catch (e) { console.error(e); }
}

function renderKeys() {
    const container = document.getElementById('keys_list');
    container.innerHTML = '';
    
    if (!currentConfig.client_keys || currentConfig.client_keys.length === 0) {
        container.innerHTML = '<div style="color: var(--text-muted); font-style: italic;">No keys generated yet.</div>';
        return;
    }

    currentConfig.client_keys.forEach((key, index) => {
        const row = document.createElement('div');
        row.style.cssText = "display: flex; justify-content: space-between; align-items: center; background: rgba(0,0,0,0.2); padding: 0.75rem; border-radius: 0.5rem;";
        row.innerHTML = `
            <code style="color: var(--success); font-family: monospace;">${key}</code>
            <button class="btn btn-danger btn-sm" onclick="deleteKey(${index})">Revoke</button>
        `;
        container.appendChild(row);
    });
}

function generateKey() {
    const key = 'sk-station-' + Math.random().toString(36).substr(2, 9) + Math.random().toString(36).substr(2, 9);
    if (!currentConfig.client_keys) currentConfig.client_keys = [];
    currentConfig.client_keys.push(key);
    saveKeys();
}

function deleteKey(index) {
    if (!confirm('Revoke this key? Clients using it will immediately lose access.')) return;
    currentConfig.client_keys.splice(index, 1);
    saveKeys();
}

let tempServiceKeys = [];

function renderServiceKeysList() {
    const list = document.getElementById('service_keys_list');
    list.innerHTML = '';
    tempServiceKeys.forEach((k, i) => {
        const row = document.createElement('div');
        row.style.cssText = "display: flex; justify-content: space-between; align-items: center; background: rgba(255,255,255,0.05); padding: 5px 10px; border-radius: 4px;";
        
        // Mask key for display
        const displayKey = k.length > 8 ? k.substr(0, 4) + '...' + k.substr(-4) : '***';
        
        row.innerHTML = `
            <span style="font-family: monospace; font-size: 0.9rem;">${displayKey}</span>
            <button style="background: none; border: none; color: #ef4444; cursor: pointer;" onclick="removeServiceKey(${i})">×</button>
        `;
        list.appendChild(row);
    });
}

function addServiceKey() {
    const input = document.getElementById('new_api_key_input');
    const val = input.value.trim();
    if (val) {
        tempServiceKeys.push(val);
        input.value = '';
        renderServiceKeysList();
    }
}

function removeServiceKey(index) {
    tempServiceKeys.splice(index, 1);
    renderServiceKeysList();
}

// --- Modal Handling ---
function openModal(editId = null) {
    document.getElementById('modal_overlay').classList.add('open');
    document.getElementById('new_api_key_input').value = '';
    
    if (editId) {
        const s = currentConfig.services.find(s => s.id === editId);
        document.getElementById('modal_title').textContent = 'Edit Service';
        document.getElementById('service_id').value = s.id;
        document.getElementById('service_name').value = s.name;
        document.getElementById('service_type').value = s.type;
        document.getElementById('base_url').value = s.base_url;
        document.getElementById('model_name').value = s.model_name;
        
        // Load Keys
        if (s.api_keys && s.api_keys.length > 0) {
            tempServiceKeys = [...s.api_keys];
        } else if (s.api_key) {
            tempServiceKeys = [s.api_key];
        } else {
            tempServiceKeys = [];
        }
    } else {
        document.getElementById('modal_title').textContent = 'Add New Service';
        document.getElementById('service_id').value = '';
        document.getElementById('service_name').value = '';
        document.getElementById('service_type').value = 'openai'; 
        document.getElementById('base_url').value = '';
        document.getElementById('model_name').value = '';
        tempServiceKeys = [];
    }
    renderServiceKeysList();
}

function closeModal() {
    document.getElementById('modal_overlay').classList.remove('open');
    tempServiceKeys = [];
}

function editService(id) { openModal(id); }

function saveService() {
    const id = document.getElementById('service_id').value;
    const newService = {
        id: id || generateUUID(),
        name: document.getElementById('service_name').value,
        type: document.getElementById('service_type').value,
        base_url: document.getElementById('base_url').value,
        api_keys: tempServiceKeys,
        api_key: tempServiceKeys.length > 0 ? tempServiceKeys[0] : "", // Legacy compat
        model_name: document.getElementById('model_name').value
    };

    if (id) {
        const idx = currentConfig.services.findIndex(s => s.id === id);
        if (idx !== -1) currentConfig.services[idx] = newService;
    } else {
        currentConfig.services.push(newService);
    }
    
    saveAllServices();
    closeModal();
}

function updatePlaceholders() {
    const type = document.getElementById('service_type').value;
    const baseInput = document.getElementById('base_url');
    if (type === 'openai') baseInput.placeholder = 'https://api.openai.com/v1';
    if (type === 'gemini') baseInput.placeholder = 'https://generativelanguage.googleapis.com/v1beta/models';
    if (type === 'anthropic') baseInput.placeholder = 'https://api.anthropic.com/v1';
}

function generateUUID() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        var r = Math.random() * 16 | 0, v = c == 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}

// --- Chat Logic ---
function updatePlaygroundDropdowns() {
    const modelSelect = document.getElementById('playground_model');
    modelSelect.innerHTML = '<option value="">Select a Model/Service...</option>';
    currentConfig.services.forEach(s => {
        const opt = document.createElement('option');
        opt.value = s.name;
        opt.textContent = s.name + (s.model_name ? ` (maps to ${s.model_name})` : '');
        modelSelect.appendChild(opt);
    });

    const keySelect = document.getElementById('playground_key');
    keySelect.innerHTML = '<option value="">Select an API Key...</option>';
    if (currentConfig.client_keys) {
        currentConfig.client_keys.forEach(k => {
            const opt = document.createElement('option');
            opt.value = k;
            opt.textContent = k;
            keySelect.appendChild(opt);
        });
    }
}

function handleEnter(e) {
    if (e.key === 'Enter') sendMessage();
}

async function sendMessage() {
    const input = document.getElementById('chat_input');
    const text = input.value.trim();
    if (!text) return;

    const model = document.getElementById('playground_model').value;
    const key = document.getElementById('playground_key').value;

    if (!model) { alert('Please select a model'); return; }
    if (!key) { alert('Please select an API Key (Authentication is now required)'); return; }

    addMessage('chat-user', text);
    input.value = '';

    try {
        const res = await fetch('/v1/chat/completions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + key
            },
            body: JSON.stringify({
                model: model,
                messages: [{role: "user", content: text}]
            })
        });

        if (!res.ok) {
            const err = await res.text();
            addMessage('chat-assistant', 'Error: ' + err);
            return;
        }

        const data = await res.json();
        const reply = data.choices[0].message.content;
        addMessage('chat-assistant', reply);

    } catch (e) {
        addMessage('chat-assistant', 'Network Error: ' + e.message);
    }
}

function addMessage(role, text) {
    const div = document.createElement('div');
    div.classList.add('chat-bubble', role);
    div.textContent = text;
    const container = document.getElementById('chat_messages');
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
}

// --- Stats Logic ---
let statsChart = null;
let tokenChart = null;

async function loadStats() {
    const date = document.getElementById('stats_date').value;
    try {
        const res = await fetch(`/api/stats?date=${date}`, {
            headers: { 'Authorization': 'Bearer ' + adminToken }
        });
        if (res.ok) {
            const data = await res.json();
            renderStats(data);
        }
    } catch (e) {
        console.error(e);
    }
}

function renderStats(data) {
    // 1. Summary
    const summaryDiv = document.getElementById('stats_summary');
    let totalTokens = 0;
    Object.values(data.summary || {}).forEach(s => totalTokens += (s.tokens_in + s.tokens_out));
    
    summaryDiv.innerHTML = `
        <div style="display: flex; gap: 3rem; text-align: center;">
            <div>
                <div style="font-size: 2rem; font-weight: bold;">${data.total_requests}</div>
                <div style="color: var(--text-muted);">Total Requests</div>
            </div>
            <div>
                <div style="font-size: 2rem; font-weight: bold;">${totalTokens.toLocaleString()}</div>
                <div style="color: var(--text-muted);">Total Tokens</div>
            </div>
        </div>
    `;

    // 2. Table
    const tbody = document.getElementById('stats_table_body');
    tbody.innerHTML = '';
    const theadRow = document.querySelector('#stats table thead tr');
    if (theadRow.children.length === 4) {
        theadRow.innerHTML += '<th style="padding: 0.5rem;">Tokens (In/Out)</th>';
    }

    if (data.records && data.records.length > 0) {
        const reversed = [...data.records].reverse().slice(0, 50);
        reversed.forEach(r => {
            const row = document.createElement('tr');
            row.style.borderBottom = "1px solid rgba(255,255,255,0.05)";
            const timeStr = new Date(r.time).toLocaleTimeString();
            const statusColor = r.success ? 'var(--success)' : '#ef4444';
            const statusIcon = r.success ? '✔' : '✘';
            
            row.innerHTML = `
                <td style="padding: 0.5rem; font-family: monospace;">${timeStr}</td>
                <td style="padding: 0.5rem;">${r.model}</td>
                <td style="padding: 0.5rem; color: ${statusColor};">${statusIcon}</td>
                <td style="padding: 0.5rem;">${r.duration_ms.toFixed(0)}ms</td>
                <td style="padding: 0.5rem; font-family: monospace; font-size: 0.9em;">
                    <span style="color: #a855f7;">${r.tokens_in || 0}</span> / 
                    <span style="color: #ec4899;">${r.tokens_out || 0}</span>
                </td>
            `;
            tbody.appendChild(row);
        });
    } else {
        tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; padding: 1rem;">No data for this date</td></tr>';
    }

    // 3. Chart
    const models = Object.keys(data.summary || {});
    const requestCounts = models.map(k => data.summary[k].requests);
    const tokensIn = models.map(k => data.summary[k].tokens_in);
    const tokensOut = models.map(k => data.summary[k].tokens_out);

    if (models.length === 0) return;

    // Request Chart
    const ctx1 = document.getElementById('chart_model_dist').getContext('2d');
    if (statsChart) statsChart.destroy();
    statsChart = new Chart(ctx1, {
        type: 'doughnut',
        data: {
            labels: models,
            datasets: [{
                data: requestCounts,
                backgroundColor: ['#6366f1', '#a855f7', '#ec4899', '#ef4444', '#f59e0b', '#10b981'],
                borderWidth: 0
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: { legend: { position: 'right', labels: { color: '#e2e8f0' } } }
        }
    });

    // Token Chart
    const ctx2 = document.getElementById('chart_token_dist').getContext('2d');
    if (tokenChart) tokenChart.destroy();
    tokenChart = new Chart(ctx2, {
        type: 'bar',
        data: {
            labels: models,
            datasets: [
                {
                    label: 'Input Tokens',
                    data: tokensIn,
                    backgroundColor: '#a855f7',
                    stack: 'Stack 0'
                },
                {
                    label: 'Output Tokens',
                    data: tokensOut,
                    backgroundColor: '#ec4899',
                    stack: 'Stack 0'
                }
            ]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: { legend: { labels: { color: '#e2e8f0' } } },
            scales: {
                x: { ticks: { color: '#94a3b8' }, grid: { display: false } },
                y: { ticks: { color: '#94a3b8' }, grid: { color: 'rgba(255,255,255,0.1)' } }
            }
        }
    });
}
