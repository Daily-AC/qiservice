const API = '/api';
let token = localStorage.getItem('token');
let currentUser = JSON.parse(localStorage.getItem('user') || '{}');
let globalServices = [];
let myKeys = [];

// Init
window.onload = async function() {
    if (!token) {
        window.location.href = 'login.html';
        return;
    }
    
    // Setup User Profile in Sidebar
    document.getElementById('disp-username').textContent = currentUser.username;
    document.getElementById('disp-role').textContent = formatRole(currentUser.role);
    document.getElementById('user-avatar').textContent = currentUser.username.charAt(0).toUpperCase();

    // Permission Check: Show/Hide Admin Nav
    if (currentUser.role === 'admin' || currentUser.role === 'super_admin') {
        document.getElementById('admin-nav').style.display = 'block';
    }

    // Role Hint based on Role
    if (document.getElementById('cu-role')) {
        const hint = document.getElementById('cu-role-hint');
        if (currentUser.role === 'super_admin') {
            hint.textContent = "超管特权：可创建管理员或普通用户。";
        } else {
            // Disable Admin option for regular admins
            const adminOpt = document.querySelector("#cu-role option[value='admin']");
            if(adminOpt) adminOpt.disabled = true;
            hint.textContent = "当前权限：仅可创建普通用户。";
        }
    }

    // Load Data
    await loadServices();
    await loadMyKeys();

    // Remove Overlay
    document.getElementById('login_check').style.display = 'none';
};

function formatRole(role) {
    if (role === 'super_admin') return '超级管理员';
    if (role === 'admin') return '管理员';
    return '普通用户';
}

function logout() {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    window.location.href = 'login.html';
}

// Navigation
function nav(page) {
    // Hide all pages
    document.querySelectorAll('.page').forEach(el => el.classList.remove('active'));
    // Deselect navs
    document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));

    // Show selected
    const target = document.getElementById('page-' + page);
    if (target) {
        target.classList.add('active');
        const navItem = document.getElementById('nav-' + page);
        if (navItem) navItem.classList.add('active');
        
        // Load data on demand
        if (page === 'my-keys') loadMyKeys();
        if (page === 'users') loadUsers();
        if (page === 'services') renderAdminServices();
        if (page === 'playground') updatePlaygroundSelects();
        
        // Refresh Dashboard on view
        if (page === 'dashboard') {
             loadDashboardStats();
             loadMyProfile();
        }
    }
}

// Global scope for onclick
window.generateMyKey = function() {
    modal.open('modal-key');
}

async function submitNewMyKey() {
    const name = document.getElementById('key-name-input').value || 'Default Key';
    try {
        const res = await fetch(API + '/my_keys', {
            method: 'POST',
            headers: { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json' },
            body: JSON.stringify({ name })
        });
        if (res.ok) {
            modal.close('modal-key');
            loadMyKeys();
        } else {
            alert("新建失败");
        }
    } catch(e) { console.error(e); }
}

// --- Data: Services ---
async function loadServices() {
    try {
        const res = await fetch(API + '/config', {
            headers: { 'Authorization': 'Bearer ' + token }
        });
        const data = await res.json();
        globalServices = data.services || [];
        renderDashboardServices();
    } catch(e) { console.error(e); }
}

function renderDashboardServices() {
    const grid = document.getElementById('service-list');
    grid.innerHTML = '';
    
    // Add Charts Container
    const statsContainer = document.createElement('div');
    statsContainer.style.gridColumn = '1 / -1';
    statsContainer.style.display = 'grid';
    statsContainer.style.gridTemplateColumns = '1fr 1fr';
    statsContainer.style.gap = '1.5rem';
    statsContainer.style.marginBottom = '2rem';
    statsContainer.innerHTML = `
        <div class="card">
            <h3>今日请求 (Requests)</h3>
            <div style="height:200px;"><canvas id="chart-req"></canvas></div>
        </div>
        <div class="card">
            <h3>Token 消耗</h3>
            <div style="height:200px;"><canvas id="chart-tok"></canvas></div>
        </div>
    `;
    grid.appendChild(statsContainer);
    
    // Quota Widget
    loadMyProfile(); 

    if (globalServices.length === 0) {
        const d = document.createElement('div');
        d.innerHTML = '<div style="color:var(--text-muted); text-align:center;">暂无服务配置</div>';
        grid.appendChild(d);
        return;
    }

    globalServices.forEach(s => {
        const div = document.createElement('div');
        div.className = 'card';
        div.style.marginBottom = '0'; // Grid handles gap
        div.innerHTML = `
            <div style="font-weight:bold; font-size:1.1rem; margin-bottom:0.5rem;">${s.name}</div>
            <div style="font-size:0.8rem; color:var(--text-muted); margin-bottom:1rem;">类型: ${s.type}</div>
            <div style="font-size:0.8rem; background:var(--bg-body); padding:0.5rem; border-radius:4px;">
                如果是通过 OpenAI SDK 调用，模型 Model 请填 <span style="color:var(--primary); font-family:monospace;">${s.name}</span>
            </div>
        `;
        grid.appendChild(div);
    });
}

function loadDashboardStats() {
    // Mock Data or Fetch Real Stats
    // Ideally fetch /api/stats. Assuming it exists and returns summary.
    fetch(API + '/stats', { headers: { 'Authorization': 'Bearer ' + token } })
        .then(r => r.json())
        .then(data => {
            renderCharts(data);
        })
        .catch(e => console.error("Stats load failed", e));
}

function renderCharts(data) {
    const totalRequests = data.total_requests || 0;
    // Simple distribution for demo if no time-series
    // Summary is by model.
    const models = Object.keys(data.summary || {});
    const reqs = models.map(m => data.summary[m].request_count);
    const tokens = models.map(m => (data.summary[m].input_tokens + data.summary[m].output_tokens));

    new Chart(document.getElementById('chart-req'), {
        type: 'doughnut',
        data: {
            labels: models,
            datasets: [{ data: reqs, backgroundColor: ['#6366f1', '#10b981', '#f59e0b', '#ef4444'] }]
        },
        options: { responsive: true, maintainAspectRatio: false }
    });

    new Chart(document.getElementById('chart-tok'), {
        type: 'bar',
        data: {
            labels: models,
            datasets: [{ label:'Tokens', data: tokens, backgroundColor: '#6366f1' }]
        },
        options: { responsive: true, maintainAspectRatio: false }
    });
}

async function loadMyProfile() {
    try {
        const res = await fetch(API + '/user/me', { headers: { 'Authorization': 'Bearer ' + token } });
        if (res.ok) {
            const u = await res.json();
            renderQuotaWidget(u);
            loadDashboardStats(); // Load charts after profile
        }
    } catch(e) { console.error(e); }
}

function renderQuotaWidget(user) {
    // Insert before Service List, or integrate into statsContainer? 
    // Let's prepend to statsContainer if exists, or finding better place.
    // Actually, let's look for a place in dashboard.
    // We can inject a new Card.
    
    const existing = document.getElementById('quota-card');
    if (existing) existing.remove(); // Refresh
    
    const container = document.getElementById('service-list');
    
    const div = document.createElement('div');
    div.id = 'quota-card';
    div.className = 'card';
    div.style.gridColumn = '1 / -1';
    div.style.background = 'linear-gradient(135deg, rgba(99,102,241,0.1) 0%, rgba(168,85,247,0.1) 100%)';
    div.style.border = '1px solid rgba(99,102,241,0.2)';
    div.style.marginBottom = '1.5rem';
    
    // Quota < 0 is unlimited. 0 is strictly 0.
    const isUnlimited = user.quota < 0;
    const used = user.used_amount;
    const remaining = isUnlimited ? '∞' : (user.quota - used).toFixed(4);
    const percent = isUnlimited ? 0 : (user.quota > 0 ? Math.min(100, (used / user.quota) * 100) : 100);
    
    div.innerHTML = `
        <div style="display:flex; justify-content:space-between; align-items:center;">
             <h3 style="margin:0; color:var(--primary);">🎟️ 配额状态 (Quota)</h3>
             <div style="font-weight:bold; font-size:1.2rem;">${isUnlimited ? '无限制 (Unlimited)' : remaining + ' 剩余 / ' + user.quota + ' 总量'}</div>
        </div>
        <div style="margin-top:1rem; background:rgba(255,255,255,0.1); border-radius:10px; height:10px; overflow:hidden;">
            <div style="width:${percent}%; background:var(--primary); height:100%; transition:width 0.5s;"></div>
        </div>
        <div style="margin-top:0.5rem; text-align:right; font-size:0.8rem; color:var(--text-muted);">
            已用: ${used.toFixed(4)} Tokens
        </div>
    `;
    
    container.insertBefore(div, container.firstChild);
}

// --- Data: My Keys ---
async function loadMyKeys() {
    // For regular users, we need an endpoint to get THEIR keys.
    // Currently API has /api/config for admins.
    // We should probably filter.
    // Actually our /api/config might return everything.
    // WAIT: User should only manage THEIR keys. 
    // We don't have a specific endpoint 'GET /api/my_keys'.
    // BUT 'GET /api/users' returns user list with keys. User can't call that.
    // Let's implement a quick heuristic:
    // If User, we assume there is no 'my_keys' endpoint yet?
    // Oh, ListUsers checks Role. If User, ListUsers fails.
    // We need an endpoint for user to get their own keys.
    
    // TEMP FIX: For now, if role is user, we can't get keys via /users.
    // We will assume the User stores keys locally or Admin gives them?
    // NO, User needs to generate keys.
    // Let's add 'GET /api/user/me' or similar?
    // Actually, 'GET /api/config' might filter keys strictly?
    // Code check: GetConfigHandler reads config.json/DB. It returns ALL client_keys legacy?
    // The new system uses `User.APIKeys`.
    
    // We need 'GET /api/keys' (List my keys) endpoint.
    // Currently we have 'POST /api/user_keys' (Generate).
    // Let's check 'internal/api/handler.go'.
    // We have 'ListUsersHandler' for Admin.
    
    // Workaround: We will fetch /api/users, but as a Regular User, it fails.
    // I will mock this for now or suggest Admin to create keys. 
    // RE-READ: Hierarchy Admin > User.
    // Let's assume for this step we only show keys for Admins via /users.
    
    // Wait, requirement is User can manage their own keys.
    // There is no endpoint for "List My Keys" in the backend yet.
    // I will add 'GET /api/my_keys' to 'handler.go' via multi_replace in next step if needed.
    // For now, let's just make the UI code ready.
    
    try {
        const res = await fetch(API + '/my_keys', {
            headers: { 'Authorization': 'Bearer ' + token }
        });
        if (res.ok) {
            const keys = await res.json(); // Expect []APIKey
            myKeys = keys;
            renderMyKeys(keys);
        } else {
            // Fallback for now
             document.getElementById('my-keys-list').innerHTML = `<tr><td colspan="4" style="text-align:center;">暂不支持获取密钥列表 (API缺)</td></tr>`;
        }
    } catch(e) { console.error(e); }
}

function renderMyKeys(keys) {
    const tbody = document.getElementById('my-keys-list');
    tbody.innerHTML = '';
    keys.forEach(k => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
            <td>${k.name || '默认密钥'}</td>
            <td>
                <div style="display:flex; align-items:center; gap:0.5rem;">
                    <code style="color:var(--success);">${k.key.substring(0,8)}...${k.key.substring(k.key.length-4)}</code>
                    <button class="btn btn-sm btn-secondary" onclick="copyText('${k.key}')" title="复制完整密钥">📋</button>
                </div>
            </td>
            <td>${k.is_active ? '✅ 正常' : '❌ 停用'}</td>
            <td>
                <button class="btn btn-sm btn-danger" onclick="deleteMyKey(${k.id})">删除</button>
            </td>
        `;
        tbody.appendChild(tr);
    });
}

function copyText(text) {
    navigator.clipboard.writeText(text).then(() => {
        alert("已复制到剪贴板");
    }).catch(err => {
        console.error('Failed to copy: ', err);
        prompt("复制失败，请手动复制:", text);
    });
}

async function deleteMyKey(id) {
    if(!confirm("确定要删除此密钥吗？")) return;
    try {
        const res = await fetch(API + '/my_keys/' + id, {
            method: 'DELETE',
            headers: { 'Authorization': 'Bearer ' + token }
        });
        if (res.ok) {
            loadMyKeys();
        } else {
            alert("删除失败");
        }
    } catch(e) { console.error(e); }
}

// --- Admin: Users ---
let globalUsers = [];
async function loadUsers() {
    try {
        const res = await fetch(API + '/users', {
            headers: { 'Authorization': 'Bearer ' + token }
        });
        if (res.ok) {
            globalUsers = await res.json();
            renderUsers();
        }
    } catch(e) { console.error(e); }
}

function renderUsers() {
    const tbody = document.getElementById('user-list-body');
    tbody.innerHTML = '';
    
    globalUsers.forEach(u => {
        const canEdit = checkEditPermission(u);
        const tr = document.createElement('tr');
        tr.innerHTML = `
            <td>${u.id}</td>
            <td>
                <div>${u.username}</div>
            </td>
            <td><span style="background:${u.role==='admin'?'var(--primary)':'var(--border-color)'}; color:${u.role==='admin'?'white':'var(--text-muted)'}; padding:2px 6px; border-radius:4px; font-size:0.8rem;">${formatRole(u.role)}</span></td>
            <td>
                <div>${u.quota} <span style="font-size:0.8rem;">总量</span></div>
                <div style="color:var(--text-muted); font-size:0.8rem;">${u.used_amount.toFixed(4)} 已用</div>
            </td>
            <td>
                <button class="btn btn-sm btn-secondary" onclick="openEditUser(${u.id})" ${canEdit?'':'disabled'}>管理</button>
                <button class="btn btn-sm btn-danger" onclick="deleteUser(${u.id})" ${checkDeletePermission(u)?'':'disabled'}>删除</button>
            </td>
        `;
        tbody.appendChild(tr);
    });
}

function checkDeletePermission(targetUser) {
    if (currentUser.role === 'super_admin') return true; 
    if (currentUser.role === 'admin' && targetUser.role === 'user') return true;
    return false;
}

async function deleteUser(id) {
    if(!confirm("确定要删除该用户吗？此操作无法撤销。")) return;
    const res = await fetch(API + '/users/' + id, {
        method: 'DELETE',
        headers: { 'Authorization': 'Bearer ' + token }
    });
    if(res.ok) loadUsers();
    else alert("删除失败 (权限不足)");
}

function checkEditPermission(targetUser) {
    if (currentUser.role === 'super_admin') return true; // SuperAdmin can edit anyone
    if (currentUser.role === 'admin') {
        if (targetUser.role === 'user') return true; // Admin can edit User
        return false; // Admin cannot edit Admin/SuperAdmin
    }
    return false;
}

// --- Admin: Services ---
function renderAdminServices() {
    const grid = document.getElementById('admin-service-list');
    grid.innerHTML = '';
    
    globalServices.forEach(s => {
        const div = document.createElement('div');
        div.className = 'card';
        // Reuse admin card style
        div.innerHTML = `
            <div style="display:flex; justify-content:space-between; margin-bottom:1rem;">
                <div style="font-weight:bold;">${s.name}</div>
                <div style="font-size:0.8rem;">${s.type}</div>
            </div>
            <div style="font-size:0.8rem; color:var(--text-muted); margin-bottom:1rem;">
                <div>Target: ${s.model_name || '(Passthrough)'}</div>
                <div>URL: ${s.base_url || 'Default'}</div>
                <div>Keys: ${s.api_keys ? s.api_keys.length : 0}</div>
            </div>
            <div style="display:flex; gap:0.5rem;">
                <button class="btn btn-sm btn-secondary" onclick="openServiceModal('${s.id}')">编辑</button>
                <button class="btn btn-sm btn-danger" onclick="deleteService('${s.id}')">删除</button>
            </div>
        `;
        grid.appendChild(div);
    });
}

// --- Modals & Actions ---
const modal = {
    open: (id) => document.getElementById(id).classList.add('open'), 
    close: (id) => document.getElementById(id).classList.remove('open')
};
window.closeModal = modal.close; // Export

function openCreateUserModal() { modal.open('modal-user'); }

async function submitCreateUser() {
    const d = { 
        username: document.getElementById('cu-username').value, 
        password: document.getElementById('cu-password').value, 
        role: document.getElementById('cu-role').value, 
        quota: parseFloat(document.getElementById('cu-quota').value) 
    };

    const res = await fetch(API+'/users', {
        method:'POST',
        headers:{'Content-Type':'application/json', 'Authorization': 'Bearer '+token},
        body:JSON.stringify(d)
    });
    
    if (res.ok) {
         modal.close('modal-user');
         loadUsers();
    } else {
        const err = await res.json();
        alert('错误: ' + err.error);
    }
}

function openEditUser(id) {
    const u = globalUsers.find(x => x.id === id);
    if (!u) return;
    document.getElementById('eu-id').value = u.id;
    document.getElementById('eu-username').value = u.username;
    document.getElementById('eu-password').value = '';
    document.getElementById('eu-quota').value = u.quota;
    document.getElementById('eu-quota').value = u.quota;
    
    // Role Edit for SuperAdmin
    const roleSelect = document.getElementById('eu-role');
    if (roleSelect) {
        if (currentUser.role === 'super_admin') {
            roleSelect.disabled = false;
            roleSelect.value = u.role;
        } else {
            roleSelect.disabled = true;
            roleSelect.value = u.role;
        }
    }
    
    modal.open('modal-edit-user');
}

async function submitEditUser() {
    const id = parseInt(document.getElementById('eu-id').value);
    const pwd = document.getElementById('eu-password').value;
    const quota = parseFloat(document.getElementById('eu-quota').value);
    const roleElem = document.getElementById('eu-role');
    
    const d = { user_id: id, quota: quota };
    if (pwd) d.password = pwd;
    if (roleElem && !roleElem.disabled) d.role = roleElem.value;

    const res = await fetch(API+'/user_update', {
        method:'POST',
        headers:{'Content-Type':'application/json', 'Authorization': 'Bearer '+token},
        body:JSON.stringify(d)
    });

    if (res.ok) {
        modal.close('modal-edit-user');
        loadUsers();
    } else {
        alert('更新失败');
    }
}

// Service Logic
// Service Logic
let tempKeys = [];

function openServiceModal(id) {
    modal.open('modal-service');
    document.getElementById('ms-new-key').value = '';
    tempKeys = [];

    if (id) {
        const s = globalServices.find(x => x.id === id);
        document.getElementById('ms-title').textContent = '编辑服务';
        document.getElementById('ms-id').value = s.id;
        document.getElementById('ms-name').value = s.name;
        document.getElementById('ms-type').value = s.type;
        document.getElementById('ms-url').value = s.base_url;
        document.getElementById('ms-map').value = s.model_name;
        // keys
        if(s.api_keys && s.api_keys.length > 0) {
            tempKeys = [...s.api_keys];
        } else if (s.api_key) {
            tempKeys = [s.api_key];
        }
    } else {
        document.getElementById('ms-title').textContent = '新建服务';
        document.getElementById('ms-id').value = '';
        document.getElementById('ms-name').value = '';
        document.getElementById('ms-url').value = '';
        document.getElementById('ms-map').value = '';
    }
    renderServiceKeys();
}

function renderServiceKeys() {
    const list = document.getElementById('ms-keys-list');
    list.innerHTML = '';
    tempKeys.forEach((k, idx) => {
        const div = document.createElement('div');
        div.style.display = 'flex';
        div.style.justifyContent = 'space-between';
        div.style.alignItems = 'center';
        div.style.marginBottom = '4px';
        div.style.padding = '4px 8px';
        div.style.background = 'var(--bg-body)';
        div.style.borderRadius = '4px';
        div.style.fontSize = '0.9rem';
        
        div.innerHTML = `
            <span style="font-family:monospace; overflow:hidden; text-overflow:ellipsis;">${k.substring(0, 12)}...${k.substring(k.length-4)}</span>
            <span style="cursor:pointer; color:#ef4444;" onclick="removeServiceKey(${idx})">🗑️</span>
        `;
        list.appendChild(div);
    });
}

function addServiceKey() {
    const input = document.getElementById('ms-new-key');
    const val = input.value.trim();
    if(val) {
        tempKeys.push(val);
        input.value = '';
        renderServiceKeys();
    }
}

function removeServiceKey(idx) {
    tempKeys.splice(idx, 1);
    renderServiceKeys();
}

async function submitService() {
    // Collect Data
    const id = document.getElementById('ms-id').value;
    
    const s = {
        id: id || uuidv4(),
        name: document.getElementById('ms-name').value,
        type: document.getElementById('ms-type').value,
        base_url: document.getElementById('ms-url').value,
        model_name: document.getElementById('ms-map').value,
        api_keys: tempKeys,
        api_key: tempKeys[0] || ''
    };

    // Update List & Save
    let list = [...globalServices];
    if (id) {
        const idx = list.findIndex(x => x.id === id);
        if (idx!==-1) list[idx] = s;
    } else {
        list.push(s);
    }
    
    await saveServices(list);
    modal.close('modal-service');
}

async function deleteService(id) {
    if (!confirm('确定删除该服务？')) return;
    const list = globalServices.filter(s => s.id !== id);
    await saveServices(list);
}

async function saveServices(list) {
    const res = await fetch(API+'/services', {
        method:'POST',
        headers:{'Content-Type':'application/json', 'Authorization': 'Bearer '+token},
        body:JSON.stringify(list)
    });
    if (res.ok) {
        await loadServices(); // reload
        // Force refresh admin view if visible
        if(document.getElementById('page-services').classList.contains('active')) {
            renderAdminServices();
        }
    } else {
        alert('保存失败');
    }
}

function uuidv4() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        var r = Math.random() * 16 | 0, v = c == 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}

// Playground
function updatePlaygroundSelects() {
    // Populate dropdowns from globalServices
    const sEl = document.getElementById('pg-model');
    sEl.innerHTML = '';
    globalServices.forEach(s => {
        const opt = document.createElement('option');
        opt.value = s.name;
        opt.textContent = s.name;
        sEl.appendChild(opt);
    });
    
    // Populate Keys
    const kEl = document.getElementById('pg-key');
    kEl.innerHTML = '';
    
    if (myKeys.length === 0) {
        kEl.innerHTML = '<option value="">(无可用密钥)</option>';
    } else {
        myKeys.forEach(k => {
            if (k.is_active) {
                const opt = document.createElement('option');
                opt.value = k.key;
                opt.textContent = k.name || k.key.substring(0, 8) + '...';
                kEl.appendChild(opt);
            }
        });
    }
}

async function sendMsg() {
    const inputEl = document.getElementById('pg-input');
    const text = inputEl.value.trim();
    if (!text) return;
    
    const model = document.getElementById('pg-model').value;
    const key = document.getElementById('pg-key').value;
    
    if (!model || !key) {
        alert("请先选择模型和密钥！");
        return;
    }

    inputEl.value = '';
    addMsg('chat-user', text);
    
    // Placeholder for assistant response
    const assistantMsgId = 'msg-' + Date.now();
    addMsg('chat-assistant', '...', assistantMsgId);
    
    try {
        const res = await fetch('/v1/chat/completions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${key}`
            },
            body: JSON.stringify({
                model: model,
                messages: [{ role: 'user', content: text }]
            })
        });

        if (!res.ok) {
            const err = await res.json();
            updateMsg(assistantMsgId, `Error: ${err.error?.message || 'Unknown error'}`);
            return;
        }

        const data = await res.json();
        const reply = data.choices?.[0]?.message?.content || '(No content)';
        updateMsg(assistantMsgId, reply);

    } catch (e) {
        console.error(e);
        updateMsg(assistantMsgId, "Request Failed: " + e.message);
    }
}

function updateMsg(id, txt) {
    const el = document.getElementById(id);
    if (el) el.textContent = txt;
}

function addMsg(cls, txt, id) {
    const d = document.createElement('div');
    d.className = 'chat-bubble ' + cls;
    d.textContent = txt;
    if (id) d.id = id;
    document.getElementById('pg-messages').appendChild(d);
    // Scroll to bottom
    const container = document.getElementById('pg-messages');
    container.scrollTop = container.scrollHeight;
}
