const API_BASE = '/api';
const token = localStorage.getItem('token');
const user = JSON.parse(localStorage.getItem('user') || '{}');

// --- Auth Check ---
function checkAuth() {
    if (!token) {
        window.location.href = 'login.html';
        return;
    }
    document.getElementById('admin-user-display').textContent = `Admin: ${user.username}`;
    
    // Default load
    loadUsers();
}

function logout() {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    window.location.href = 'login.html';
}

// --- Navigation ---
function switchTab(tabId) {
    document.querySelectorAll('.page').forEach(el => el.classList.remove('active'));
    document.getElementById('tab-' + tabId).classList.add('active');
    
    document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
    event.target.classList.add('active');

    if (tabId === 'users') loadUsers();
    if (tabId === 'stats') loadStats();
    if (tabId === 'services') loadServices();
}

// --- Users Management ---
async function loadUsers() {
    try {
        const res = await fetch(`${API_BASE}/users`, {
            headers: { 'Authorization': 'Bearer ' + token }
        });
        if (res.status === 401) { logout(); return; }
        
        const users = await res.json();
        renderUsers(users);
    } catch (e) {
        console.error("Failed to load users", e);
    }
}

function renderUsers(users) {
    const tbody = document.getElementById('users-table-body');
    tbody.innerHTML = '';

    users.forEach(u => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
            <td>${u.id}</td>
            <td>
                <div style="font-weight: 600;">${u.username}</div>
                <div style="font-size: 0.8rem; color: var(--text-muted);">${u.api_keys ? u.api_keys.length : 0} Keys</div>
            </td>
            <td>
                <span style="background: ${u.role==='admin'?'var(--primary)':'rgba(255,255,255,0.1)'}; padding: 2px 8px; border-radius: 4px; font-size: 0.8rem;">
                    ${u.role}
                </span>
            </td>
            <td>${u.role === 'admin' ? 'Unlimited' : u.quota.toLocaleString()}</td>
            <td>${u.used_amount.toLocaleString()}</td>
            <td>
                <button class="btn btn-sm btn-secondary" onclick="openKeyModal(${u.id}, '${u.username}')">
                    <i class="fas fa-key"></i> New Key
                </button>
            </td>
        `;
        tbody.appendChild(tr);
    });
}

// --- Service Management ---
let currentServices = [];

async function loadServices() {
    try {
        const res = await fetch(`${API_BASE}/config`, {  // Reuse existing GET /config for listing? Or create separate list? /config returns full JSON including services.
            headers: { 'Authorization': 'Bearer ' + token }
        });
        const data = await res.json();
        currentServices = data.services || [];
        renderServices();
    } catch(e) { console.error(e); }
}

function renderServices() {
    const grid = document.getElementById('admin_service_grid');
    grid.innerHTML = '';
    currentServices.forEach(s => {
        const card = document.createElement('div');
        card.className = 'service-card'; // Reuse style from index? Check styles.
        // Assuming style.css is shared.
        card.innerHTML = `
             <div class="service-header">
                <div>
                    <div class="service-name" style="font-weight: bold; font-size: 1.2rem;">${s.name}</div>
                    <div class="service-type" style="color: var(--text-muted); font-size: 0.9rem;">${s.type}</div>
                </div>
                 <div class="service-actions">
                    <button class="btn btn-secondary btn-sm" onclick="openServiceModal('${s.id}')">Edit</button>
                    <button class="btn btn-danger btn-sm" onclick="deleteService('${s.id}')">Delete</button>
                </div>
            </div>
            <div class="service-details" style="margin-top: 1rem; font-size: 0.9rem;">
                <div style="display: flex; justify-content: space-between; margin-bottom: 4px;">
                    <span>Base URL:</span>
                    <span style="opacity: 0.7;">${s.base_url || 'Default'}</span>
                </div>
                <div style="display: flex; justify-content: space-between;">
                    <span>Target Model:</span>
                    <span style="color: var(--primary);">${s.model_name || '(Passthrough)'}</span>
                </div>
                 <div style="margin-top: 0.5rem; color: var(--text-muted);">
                    ${s.api_keys ? s.api_keys.length : 0} API Keys
                </div>
            </div>
        `;
        grid.appendChild(card);
    });
}

function openServiceModal(editId = null) {
    document.getElementById('service-modal').classList.add('open');
    if (editId) {
        const s = currentServices.find(x => x.id === editId);
        document.getElementById('modal_title').textContent = 'Edit Service';
        document.getElementById('service_id').value = s.id;
        document.getElementById('service_name').value = s.name;
        document.getElementById('service_type').value = s.type;
        document.getElementById('base_url').value = s.base_url;
        document.getElementById('model_name').value = s.model_name;
        document.getElementById('service_keys_raw').value = s.api_keys ? s.api_keys.join('\n') : (s.api_key || '');
    } else {
        document.getElementById('modal_title').textContent = 'Add Service';
        document.getElementById('service_id').value = '';
        document.getElementById('service_name').value = '';
        document.getElementById('service_type').value = 'openai';
        document.getElementById('base_url').value = '';
        document.getElementById('model_name').value = '';
        document.getElementById('service_keys_raw').value = '';
    }
}

async function submitSaveService() {
    const id = document.getElementById('service_id').value;
    const name = document.getElementById('service_name').value;
    const type = document.getElementById('service_type').value;
    const baseUrl = document.getElementById('base_url').value;
    const modelName = document.getElementById('model_name').value;
    const keysRaw = document.getElementById('service_keys_raw').value;
    
    // Parse keys (split by newline and trim)
    const keys = keysRaw.split('\n').map(k => k.trim()).filter(k => k !== '');

    const serviceObj = {
        id: id || generateUUID(),
        name,
        type,
        base_url: baseUrl,
        api_keys: keys,
        api_key: keys.length > 0 ? keys[0] : "", // legacy compat
        model_name: modelName
    };

    // Update local list then save ALL
    let newServices = [...currentServices];
    if (id) {
        const idx = newServices.findIndex(x => x.id === id);
        if (idx !== -1) newServices[idx] = serviceObj;
    } else {
        newServices.push(serviceObj);
    }

    try {
        const res = await fetch(`${API_BASE}/services`, {
            method: 'POST',
            headers: {
                 'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify(newServices)
        });
        if (res.ok) {
            closeModal('service-modal');
            loadServices();
        } else {
            alert('Failed to save service');
        }
    } catch(e) { console.error(e); }
}

async function deleteService(id) {
    if (!confirm('Delete this service?')) return;
    const newServices = currentServices.filter(s => s.id !== id);
     try {
        const res = await fetch(`${API_BASE}/services`, {
            method: 'POST',
            headers: {
                 'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify(newServices)
        });
        if (res.ok) {
            loadServices();
        }
    } catch(e) { console.error(e); }
}

function generateUUID() {
     return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        var r = Math.random() * 16 | 0, v = c == 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}

// --- Create User ---
function openCreateUserModal() {
    document.getElementById('create-user-modal').classList.add('open');
}

async function submitCreateUser() {
    const username = document.getElementById('new-username').value;
    const password = document.getElementById('new-password').value;
    const role = document.getElementById('new-role').value;
    const quota = parseInt(document.getElementById('new-quota').value);

    if (!username || !password) return alert("Username and Password required");

    try {
        const res = await fetch(`${API_BASE}/users`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify({ username, password, role, quota })
        });

        if (res.ok) {
            closeModal('create-user-modal');
            loadUsers();
            // Clear form
            document.getElementById('new-username').value = '';
            document.getElementById('new-password').value = '';
        } else {
            const err = await res.json();
            alert("Error: " + err.error);
        }
    } catch (e) {
        alert("Network Error");
    }
}

// --- Generate Key ---
function openKeyModal(userId, username) {
    document.getElementById('target-user-id').value = userId;
    document.getElementById('new-key-name').placeholder = `${username}'s Key`;
    document.getElementById('key-result-area').style.display = 'none';
    document.getElementById('btn-gen-key').style.display = 'block';
    document.getElementById('generate-key-modal').classList.add('open');
}

async function submitGenerateKey() {
    const userId = parseInt(document.getElementById('target-user-id').value);
    const name = document.getElementById('new-key-name').value || "Default API Key";

    try {
        const res = await fetch(`${API_BASE}/user_keys`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify({ user_id: userId, name })
        });

        if (res.ok) {
            const data = await res.json();
            document.getElementById('generated-key-display').textContent = data.key;
            document.getElementById('key-result-area').style.display = 'block';
            document.getElementById('btn-gen-key').style.display = 'none'; // Hide generate button to prevent double click, force close
            loadUsers(); 
        } else {
            alert("Failed to generate key");
        }
    } catch (e) {
        alert("Network Error");
    }
}

// --- Stats (Reuse existing logic simplified) ---
async function loadStats() {
    const res = await fetch(`${API_BASE}/stats`, {
        headers: { 'Authorization': 'Bearer ' + token }
    });
    const data = await res.json();
    document.getElementById('stat-total-req').textContent = data.total_requests || 0;
    
    // Calculate totals from summary if available
    let tokensIn = 0, tokensOut = 0;
    if (data.summary) {
        Object.values(data.summary).forEach(s => {
            tokensIn += s.input_tokens || 0;
            tokensOut += s.output_tokens || 0;
        });
    }
    document.getElementById('stat-tokens-in').textContent = tokensIn.toLocaleString();
    document.getElementById('stat-tokens-out').textContent = tokensOut.toLocaleString();
}

// --- Utils ---
function closeModal(id) {
    document.getElementById(id).classList.remove('open');
}

window.onclick = function(event) {
    if (event.target.classList.contains('modal-overlay')) {
        event.target.classList.remove('open');
    }
}

// Init
window.onload = checkAuth;
