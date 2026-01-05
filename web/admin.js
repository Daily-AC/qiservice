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
    
    // Verify token validity by fetching users
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
    // Assuming backend returns Total Req, etc.
    // Actually current /api/stats returns { date: string, records: [], summary: {}, total_requests: int }
    // It is "Daily Stats".
    // For "System Analytics" we might want global totals.
    // But let's show what we have.
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
