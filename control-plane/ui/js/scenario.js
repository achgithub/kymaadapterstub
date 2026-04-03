// Scenario detail logic

let currentScenario = null;
let adapterTypesByName = {};
let pollInterval = null;

document.addEventListener('DOMContentLoaded', async () => {
    const params = new URLSearchParams(window.location.search);
    const scenarioId = params.get('id');

    if (!scenarioId) {
        alert('No scenario ID provided');
        window.location.href = '/';
        return;
    }

    await loadScenario(scenarioId);
    setupEventListeners();
});

function setupEventListeners() {
    document.getElementById('adapterType').addEventListener('change', updateConfigSections);
    document.getElementById('createAdapterBtn').addEventListener('click', createAdapter);
    document.getElementById('launchBtn').addEventListener('click', launchScenario);
    const stopBtnEl = document.getElementById('stopBtn');
    if (stopBtnEl) stopBtnEl.addEventListener('click', stopScenario);
    document.getElementById('deleteBtn').addEventListener('click', deleteScenarioConfirm);
    document.getElementById('updateAdapterBtn').addEventListener('click', updateAdapter);
    document.getElementById('addFileBtn').addEventListener('click', addFileInput);
}

async function loadScenario(scenarioId) {
    try {
        currentScenario = await api.getScenario(scenarioId);
        displayScenario(currentScenario);

        // Store adapter types by ID for later use
        currentScenario.adapters.forEach(adapter => {
            adapterTypesByName[adapter.id] = adapter.type;
        });

        // Poll for updates every 3 seconds (only create interval once)
        if (!pollInterval) {
            pollInterval = setInterval(() => refreshScenario(scenarioId), 3000);
        }
    } catch (error) {
        console.error('Failed to load scenario:', error);
        alert(`Failed to load scenario: ${error.message}`);
    }
}

async function refreshScenario(scenarioId) {
    try {
        const scenario = await api.getScenario(scenarioId);
        displayScenario(scenario);
    } catch (error) {
        console.error('Failed to refresh scenario:', error);
    }
}

function displayScenario(scenario) {
    document.getElementById('scenarioName').textContent = escapeHtml(scenario.name);
    document.getElementById('scenarioStatus').textContent = scenario.status.toUpperCase();
    document.getElementById('scenarioStatus').className = `badge ${getStatusBadgeClass(scenario.status)}`;
    document.getElementById('scenarioCreated').textContent = new Date(scenario.created_at).toLocaleString();

    // Show launch/stop buttons based on status
    const launchBtn = document.getElementById('launchBtn');
    const stopBtn = document.getElementById('stopBtn');
    if (launchBtn) launchBtn.style.display = scenario.status === 'stopped' ? 'inline-block' : 'none';
    if (stopBtn) stopBtn.style.display = scenario.status === 'running' ? 'inline-block' : 'none';

    // GitHub scenarios: show Clone button, hide Delete and Export; user scenarios: show Export and Delete
    const isGithub = scenario.source === 'github';
    const exportBtn = document.getElementById('exportBtn');
    const cloneBtn = document.getElementById('cloneBtn');
    const deleteBtn = document.getElementById('deleteBtn');
    if (exportBtn) exportBtn.style.display = isGithub ? 'none' : 'inline-block';
    if (cloneBtn) cloneBtn.style.display = isGithub ? 'inline-block' : 'none';
    if (deleteBtn) deleteBtn.style.display = isGithub ? 'none' : 'inline-block';

    // Display adapters
    if (scenario.adapters.length === 0) {
        document.getElementById('adaptersList').style.display = 'none';
        document.getElementById('noAdapters').style.display = 'block';
    } else {
        document.getElementById('adaptersList').style.display = 'block';
        document.getElementById('noAdapters').style.display = 'none';

        const tbody = document.getElementById('adaptersTableBody');
        tbody.innerHTML = scenario.adapters.map(adapter => `
            <tr>
                <td><strong>${escapeHtml(adapter.name)}</strong></td>
                <td><span class="badge bg-primary">${adapter.type}</span></td>
                <td><span class="badge ${getStatusBadgeClass(adapter.status)}">${adapter.status.toUpperCase()}</span></td>
                <td>
                    ${adapter.ingress_url ? `
                        <small class="url-text">${escapeHtml(adapter.ingress_url)}</small>
                        <button class="btn btn-link copy-btn" onclick="copyToClipboard('${adapter.ingress_url}')">copy</button>
                    ` : '<small class="text-muted">Not assigned</small>'}
                </td>
                <td>${adapter.behavior_mode}</td>
                <td><small class="text-muted">${formatLastActivity(adapter.last_activity)}</small></td>
                <td>
                    <button class="btn btn-sm btn-warning" onclick="editAdapter('${adapter.id}')">Edit</button>
                    ${adapter.status === 'running' ? `<button class="btn btn-sm btn-info" onclick="showAdapterLogs('${adapter.id}', '${escapeHtml(adapter.name)}')">Logs</button>` : ''}
                    ${(adapter.status === 'running' && SENDER_TYPES.includes(adapter.type)) ? `<button class="btn btn-sm btn-success" onclick="fireAdapter('${adapter.id}', '${escapeHtml(adapter.name)}')">Fire</button>` : ''}
                    ${adapter.status === 'running' ? `<button class="btn btn-sm btn-secondary" onclick="stopAdapter('${adapter.id}')">Stop</button>` : ''}
                    <button class="btn btn-sm btn-danger" onclick="deleteAdapter('${adapter.id}')">Delete</button>
                </td>
            </tr>
        `).join('');
    }

    currentScenario = scenario;
}

const SENDER_TYPES = ['REST-SENDER', 'SOAP-SENDER', 'XI-SENDER'];

function updateConfigSections() {
    const type = document.getElementById('adapterType').value;
    const isSender = SENDER_TYPES.includes(type);
    document.getElementById('restConfig').style.display = type === 'REST' ? 'block' : 'none';
    document.getElementById('sftpConfig').style.display = type === 'SFTP' ? 'block' : 'none';
    document.getElementById('odataConfig').style.display = type === 'OData' ? 'block' : 'none';
    document.getElementById('soapConfig').style.display = (type === 'SOAP' || type === 'XI') ? 'block' : 'none';
    document.getElementById('as2Config').style.display = type === 'AS2' ? 'block' : 'none';
    document.getElementById('as4Config').style.display = type === 'AS4' ? 'block' : 'none';
    document.getElementById('edifactConfig').style.display = type === 'EDIFACT' ? 'block' : 'none';
    document.getElementById('senderConfig').style.display = isSender ? 'block' : 'none';
    // Hide behavior mode for sender adapters — they don't use it
    document.getElementById('behaviorMode').closest('.mb-3').style.display = isSender ? 'none' : 'block';
}

async function createAdapter() {
    const name = document.getElementById('adapterName').value.trim();
    const type = document.getElementById('adapterType').value;
    const behaviorMode = document.getElementById('behaviorMode').value;

    if (!name || !type) {
        alert('Please enter adapter name and select type');
        return;
    }

    let config = {};
    let credentials = null;

    if (type === 'REST') {
        try {
            config = {
                status_code: parseInt(document.getElementById('restStatusCode').value),
                response_body: document.getElementById('restBody').value,
                response_headers: JSON.parse(document.getElementById('restHeaders').value || '{}'),
            };
        } catch (e) {
            alert('Invalid JSON in headers');
            return;
        }
    } else if (type === 'SFTP') {
        const username = document.getElementById('sftpUsername').value;
        const password = document.getElementById('sftpPassword').value;

        credentials = { username, password };

        const files = [];
        document.querySelectorAll('.file-input-group').forEach(group => {
            const fileName = group.querySelector('.file-name').value;
            const fileContent = group.querySelector('.file-content').value;
            if (fileName && fileContent) {
                files.push({ name: fileName, content: fileContent });
            }
        });

        config = {
            files,
            auth_mode: behaviorMode === 'failure' ? 'failure' : 'success',
        };
    } else if (type === 'OData') {
        config = {
            status_code: parseInt(document.getElementById('odataStatusCode').value),
            response_body: document.getElementById('odataBody').value,
            response_headers: {},
        };
    } else if (type === 'SOAP' || type === 'XI') {
        config = {
            status_code: parseInt(document.getElementById('soapStatusCode').value),
            response_body: document.getElementById('soapBody').value,
            soap_version: document.getElementById('soapVersion').value,
            response_headers: {},
        };
    } else if (type === 'AS2') {
        config = {
            status_code: parseInt(document.getElementById('as2StatusCode').value),
            response_body: document.getElementById('as2Body').value,
            as2_from: document.getElementById('as2From').value,
            as2_to: document.getElementById('as2To').value,
            response_headers: {},
        };
    } else if (type === 'AS4') {
        config = {
            status_code: parseInt(document.getElementById('as4StatusCode').value),
            response_body: document.getElementById('as4Body').value,
            as4_party_id: document.getElementById('as4PartyId').value,
            response_headers: {},
        };
    } else if (type === 'EDIFACT') {
        config = {
            status_code: parseInt(document.getElementById('ediStatusCode').value),
            response_body: document.getElementById('ediBody').value,
            edi_standard: document.getElementById('ediStandard').value,
            response_headers: {},
        };
    } else if (SENDER_TYPES.includes(type)) {
        try {
            config = {
                target_url: document.getElementById('senderTargetURL').value,
                method: document.getElementById('senderMethod').value,
                request_body: document.getElementById('senderBody').value,
                request_headers: JSON.parse(document.getElementById('senderHeaders').value || '{}'),
            };
        } catch (e) {
            alert('Invalid JSON in request headers');
            return;
        }
    }

    try {
        const adapter = await api.addAdapter(currentScenario.id, {
            name,
            type,
            behavior_mode: behaviorMode,
            config,
            credentials,
        });

        // Reset form
        document.getElementById('adapterName').value = '';
        document.getElementById('adapterType').value = '';
        document.getElementById('behaviorMode').value = 'success';
        bootstrap.Modal.getInstance(document.getElementById('newAdapterModal')).hide();

        // Reload scenario
        await loadScenario(currentScenario.id);
    } catch (error) {
        alert(`Failed to create adapter: ${error.message}`);
    }
}

async function deleteAdapter(adapterId) {
    if (!confirm('Delete this adapter?')) {
        return;
    }

    try {
        await api.deleteAdapter(currentScenario.id, adapterId);
        await loadScenario(currentScenario.id);
    } catch (error) {
        alert(`Failed to delete adapter: ${error.message}`);
    }
}

function editAdapter(adapterId) {
    const adapter = currentScenario.adapters.find(a => a.id === adapterId);
    if (!adapter) return;

    document.getElementById('editAdapterId').value = adapterId;
    document.getElementById('editBehaviorMode').value = adapter.behavior_mode;

    const c = adapter.config || {};
    let configHtml = '';

    if (adapter.type === 'REST' || adapter.type === 'OData') {
        configHtml = `
            <div class="mb-3">
                <label class="form-label">Status Code</label>
                <input type="number" class="form-control" id="editStatusCode" value="${c.status_code || 200}">
            </div>
            <div class="mb-3">
                <label class="form-label">Response Body</label>
                <textarea class="form-control" id="editBody" rows="5">${escapeHtml(c.response_body || '')}</textarea>
            </div>
            <div class="mb-3">
                <label class="form-label">Response Headers (JSON)</label>
                <textarea class="form-control" id="editHeaders" rows="2">${escapeHtml(JSON.stringify(c.response_headers || {}, null, 2))}</textarea>
            </div>`;
    } else if (adapter.type === 'SFTP') {
        const files = (c.files || []).map(f => `
            <div class="file-input-group card card-sm mb-2 p-2">
                <div class="row g-2">
                    <div class="col-md-4">
                        <input type="text" class="form-control form-control-sm file-name" value="${escapeHtml(f.name || '')}" placeholder="Filename">
                    </div>
                    <div class="col-md-8">
                        <div class="input-group input-group-sm">
                            <textarea class="form-control form-control-sm file-content" rows="2">${escapeHtml(f.content || '')}</textarea>
                            <button type="button" class="btn btn-danger btn-sm" onclick="this.closest('.file-input-group').remove()">×</button>
                        </div>
                    </div>
                </div>
            </div>`).join('');
        const fingerprintHtml = c.ssh_host_key_fingerprint
            ? `<div class="mb-3">
                <label class="form-label">SSH Host Key Fingerprint</label>
                <div class="input-group">
                    <input type="text" class="form-control form-control-sm font-monospace" value="${escapeHtml(c.ssh_host_key_fingerprint)}" readonly>
                    <button class="btn btn-outline-secondary btn-sm" type="button" onclick="copyToClipboard('${escapeHtml(c.ssh_host_key_fingerprint)}')">Copy</button>
                </div>
                <small class="form-text text-muted">Add this to your CPI SFTP channel to avoid host key warnings on reconnect.</small>
               </div>`
            : '';
        configHtml = `
            ${fingerprintHtml}
            <div class="mb-3">
                <label class="form-label">Files</label>
                <div id="editFilesList">${files}</div>
                <button type="button" class="btn btn-sm btn-secondary mt-2" onclick="addEditFileInput()">+ Add File</button>
            </div>`;
    } else if (adapter.type === 'SOAP' || adapter.type === 'XI') {
        configHtml = `
            <div class="mb-3">
                <label class="form-label">SOAP Version</label>
                <select class="form-select" id="editSoapVersion">
                    <option value="1.1" ${(c.soap_version || '1.1') === '1.1' ? 'selected' : ''}>SOAP 1.1 (text/xml)</option>
                    <option value="1.2" ${c.soap_version === '1.2' ? 'selected' : ''}>SOAP 1.2 (application/soap+xml)</option>
                </select>
            </div>
            <div class="mb-3">
                <label class="form-label">Status Code</label>
                <input type="number" class="form-control" id="editStatusCode" value="${c.status_code || 200}">
            </div>
            <div class="mb-3">
                <label class="form-label">Response Body (SOAP XML)</label>
                <textarea class="form-control" id="editBody" rows="5">${escapeHtml(c.response_body || '')}</textarea>
            </div>`;
    } else if (adapter.type === 'AS2') {
        configHtml = `
            <div class="mb-3">
                <label class="form-label">AS2-From (expected sender ID)</label>
                <input type="text" class="form-control" id="editAs2From" value="${escapeHtml(c.as2_from || '')}" placeholder="Leave blank to accept any">
            </div>
            <div class="mb-3">
                <label class="form-label">AS2-To (our ID)</label>
                <input type="text" class="form-control" id="editAs2To" value="${escapeHtml(c.as2_to || 'KYMA_STUB')}">
            </div>
            <div class="mb-3">
                <label class="form-label">Status Code</label>
                <input type="number" class="form-control" id="editStatusCode" value="${c.status_code || 200}">
            </div>
            <div class="mb-3">
                <label class="form-label">Custom Response Body</label>
                <textarea class="form-control" id="editBody" rows="3">${escapeHtml(c.response_body || '')}</textarea>
            </div>`;
    } else if (adapter.type === 'AS4') {
        configHtml = `
            <div class="mb-3">
                <label class="form-label">Our Party ID (ebMS3)</label>
                <input type="text" class="form-control" id="editAs4PartyId" value="${escapeHtml(c.as4_party_id || 'KYMA_STUB')}">
            </div>
            <div class="mb-3">
                <label class="form-label">Status Code</label>
                <input type="number" class="form-control" id="editStatusCode" value="${c.status_code || 200}">
            </div>
            <div class="mb-3">
                <label class="form-label">Custom Response Body (SOAP XML)</label>
                <textarea class="form-control" id="editBody" rows="3">${escapeHtml(c.response_body || '')}</textarea>
            </div>`;
    } else if (adapter.type === 'EDIFACT') {
        configHtml = `
            <div class="mb-3">
                <label class="form-label">EDI Standard</label>
                <select class="form-select" id="editEdiStandard">
                    <option value="" ${!c.edi_standard ? 'selected' : ''}>Auto-detect from body</option>
                    <option value="EDIFACT" ${c.edi_standard === 'EDIFACT' ? 'selected' : ''}>EDIFACT</option>
                    <option value="X12" ${c.edi_standard === 'X12' ? 'selected' : ''}>X12 (ANSI)</option>
                </select>
            </div>
            <div class="mb-3">
                <label class="form-label">Status Code</label>
                <input type="number" class="form-control" id="editStatusCode" value="${c.status_code || 200}">
            </div>
            <div class="mb-3">
                <label class="form-label">Custom Acknowledgement Body</label>
                <textarea class="form-control" id="editBody" rows="3">${escapeHtml(c.response_body || '')}</textarea>
            </div>`;
    } else if (SENDER_TYPES.includes(adapter.type)) {
        configHtml = `
            <div class="mb-3">
                <label class="form-label">Target URL</label>
                <input type="text" class="form-control" id="editSenderTargetURL" value="${escapeHtml(c.target_url || '')}">
            </div>
            <div class="mb-3">
                <label class="form-label">HTTP Method</label>
                <select class="form-select" id="editSenderMethod">
                    <option value="POST" ${(c.method || 'POST') === 'POST' ? 'selected' : ''}>POST</option>
                    <option value="GET" ${c.method === 'GET' ? 'selected' : ''}>GET</option>
                    <option value="PUT" ${c.method === 'PUT' ? 'selected' : ''}>PUT</option>
                    <option value="PATCH" ${c.method === 'PATCH' ? 'selected' : ''}>PATCH</option>
                </select>
            </div>
            <div class="mb-3">
                <label class="form-label">Request Body</label>
                <textarea class="form-control" id="editSenderBody" rows="4">${escapeHtml(c.request_body || '')}</textarea>
            </div>
            <div class="mb-3">
                <label class="form-label">Request Headers (JSON)</label>
                <textarea class="form-control" id="editSenderHeaders" rows="2">${escapeHtml(JSON.stringify(c.request_headers || {}, null, 2))}</textarea>
            </div>`;
    }

    document.getElementById('editConfigContainer').innerHTML = configHtml;
    new bootstrap.Modal(document.getElementById('editAdapterModal')).show();
}

function addEditFileInput() {
    const container = document.getElementById('editFilesList');
    container.insertAdjacentHTML('beforeend', `
        <div class="file-input-group card card-sm mb-2 p-2">
            <div class="row g-2">
                <div class="col-md-4">
                    <input type="text" class="form-control form-control-sm file-name" placeholder="Filename">
                </div>
                <div class="col-md-8">
                    <div class="input-group input-group-sm">
                        <textarea class="form-control form-control-sm file-content" rows="2" placeholder="Content"></textarea>
                        <button type="button" class="btn btn-danger btn-sm" onclick="this.closest('.file-input-group').remove()">×</button>
                    </div>
                </div>
            </div>
        </div>`);
}

async function updateAdapter() {
    const adapterId = document.getElementById('editAdapterId').value;
    const adapter = currentScenario.adapters.find(a => a.id === adapterId);
    if (!adapter) return;

    const behaviorMode = document.getElementById('editBehaviorMode').value;
    let config = { ...adapter.config };

    if (adapter.type === 'REST' || adapter.type === 'OData') {
        config.status_code = parseInt(document.getElementById('editStatusCode').value);
        config.response_body = document.getElementById('editBody').value;
        try {
            config.response_headers = JSON.parse(document.getElementById('editHeaders').value || '{}');
        } catch (e) {
            alert('Invalid JSON in headers');
            return;
        }
    } else if (adapter.type === 'SFTP') {
        const files = [];
        document.querySelectorAll('#editFilesList .file-input-group').forEach(group => {
            const name = group.querySelector('.file-name').value;
            const content = group.querySelector('.file-content').value;
            if (name && content) files.push({ name, content });
        });
        config.files = files;
        config.auth_mode = behaviorMode === 'failure' ? 'failure' : 'success';
    } else if (adapter.type === 'SOAP' || adapter.type === 'XI') {
        config.status_code = parseInt(document.getElementById('editStatusCode').value);
        config.response_body = document.getElementById('editBody').value;
        config.soap_version = document.getElementById('editSoapVersion').value;
    } else if (adapter.type === 'AS2') {
        config.status_code = parseInt(document.getElementById('editStatusCode').value);
        config.response_body = document.getElementById('editBody').value;
        config.as2_from = document.getElementById('editAs2From').value;
        config.as2_to = document.getElementById('editAs2To').value;
    } else if (adapter.type === 'AS4') {
        config.status_code = parseInt(document.getElementById('editStatusCode').value);
        config.response_body = document.getElementById('editBody').value;
        config.as4_party_id = document.getElementById('editAs4PartyId').value;
    } else if (adapter.type === 'EDIFACT') {
        config.status_code = parseInt(document.getElementById('editStatusCode').value);
        config.response_body = document.getElementById('editBody').value;
        config.edi_standard = document.getElementById('editEdiStandard').value;
    } else if (SENDER_TYPES.includes(adapter.type)) {
        config.target_url = document.getElementById('editSenderTargetURL').value;
        config.method = document.getElementById('editSenderMethod').value;
        config.request_body = document.getElementById('editSenderBody').value;
        try {
            config.request_headers = JSON.parse(document.getElementById('editSenderHeaders').value || '{}');
        } catch (e) {
            alert('Invalid JSON in request headers');
            return;
        }
    }

    try {
        await api.updateAdapter(currentScenario.id, adapterId, {
            behavior_mode: behaviorMode,
            config,
        });
        bootstrap.Modal.getInstance(document.getElementById('editAdapterModal')).hide();
        await loadScenario(currentScenario.id);
    } catch (error) {
        alert(`Failed to update adapter: ${error.message}`);
    }
}

async function launchScenario() {
    if (!confirm('Launch all adapters in this scenario?')) {
        return;
    }

    try {
        document.getElementById('launchBtn').disabled = true;
        document.getElementById('launchBtn').textContent = 'Launching...';

        await api.launchScenario(currentScenario.id);
        await loadScenario(currentScenario.id);

        document.getElementById('launchBtn').disabled = false;
        document.getElementById('launchBtn').textContent = '🚀 Launch All Adapters';
    } catch (error) {
        alert(`Failed to launch scenario: ${error.message}`);
        document.getElementById('launchBtn').disabled = false;
        document.getElementById('launchBtn').textContent = '🚀 Launch All Adapters';
    }
}

async function stopScenario() {
    if (!confirm('Stop all running adapters in this scenario?')) {
        return;
    }

    try {
        document.getElementById('stopBtn').disabled = true;
        document.getElementById('stopBtn').textContent = 'Stopping...';

        await api.stopScenario(currentScenario.id);
        await loadScenario(currentScenario.id);

        document.getElementById('stopBtn').disabled = false;
        document.getElementById('stopBtn').textContent = '⏹ Stop All Adapters';
    } catch (error) {
        alert(`Failed to stop scenario: ${error.message}`);
        document.getElementById('stopBtn').disabled = false;
        document.getElementById('stopBtn').textContent = '⏹ Stop All Adapters';
    }
}

async function stopAdapter(adapterId) {
    if (!confirm('Stop this adapter?')) {
        return;
    }

    try {
        await api.stopAdapter(currentScenario.id, adapterId);
        await loadScenario(currentScenario.id);
    } catch (error) {
        alert(`Failed to stop adapter: ${error.message}`);
    }
}

async function deleteScenarioConfirm() {
    if (!confirm('Delete this scenario and all its adapters? This cannot be undone.')) {
        return;
    }

    try {
        if (pollInterval) {
            clearInterval(pollInterval);
        }
        await api.deleteScenario(currentScenario.id);
        window.location.href = '/';
    } catch (error) {
        alert(`Failed to delete scenario: ${error.message}`);
    }
}

function addFileInput() {
    const container = document.getElementById('filesList');
    const count = container.querySelectorAll('.file-input-group').length;

    const html = `
        <div class="file-input-group card card-sm mb-2 p-2">
            <div class="row g-2">
                <div class="col-md-4">
                    <input type="text" class="form-control form-control-sm file-name" placeholder="Filename">
                </div>
                <div class="col-md-8">
                    <div class="input-group input-group-sm">
                        <textarea class="form-control form-control-sm file-content" rows="2" placeholder="Content"></textarea>
                        <button type="button" class="btn btn-danger btn-sm" onclick="this.closest('.file-input-group').remove()">×</button>
                    </div>
                </div>
            </div>
        </div>
    `;

    container.insertAdjacentHTML('beforeend', html);
}

function copyToClipboard(text) {
    navigator.clipboard.writeText(text).then(() => {
        alert('Copied to clipboard!');
    });
}

function getStatusBadgeClass(status) {
    switch (status) {
        case 'running':
            return 'bg-success';
        case 'stopped':
            return 'bg-secondary';
        case 'starting':
            return 'bg-warning';
        default:
            return 'bg-secondary';
    }
}

function formatLastActivity(lastActivity) {
    if (!lastActivity) return '—';
    const diff = Math.floor((Date.now() - new Date(lastActivity)) / 1000);
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    return `${Math.floor(diff / 3600)}h ago`;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// ---- Pod Logs ----

let currentLogsAdapterId = null;

async function showAdapterLogs(adapterId, adapterName) {
    currentLogsAdapterId = adapterId;
    document.getElementById('logsAdapterName').textContent = adapterName;
    document.getElementById('adapterLogsContent').textContent = '(loading...)';
    new bootstrap.Modal(document.getElementById('adapterLogsModal')).show();
    await refreshAdapterLogs();
}

async function refreshAdapterLogs() {
    if (!currentLogsAdapterId) return;
    const tail = document.getElementById('logsTailInput').value || 100;
    const content = document.getElementById('adapterLogsContent');
    try {
        const resp = await fetch(`/api/scenarios/${currentScenario.id}/adapters/${currentLogsAdapterId}/logs?tail=${tail}`);
        if (!resp.ok) {
            content.textContent = `Error: HTTP ${resp.status}`;
            return;
        }
        const data = await resp.json();
        content.textContent = data.logs || '(no log output)';
        content.scrollTop = content.scrollHeight;
    } catch (e) {
        content.textContent = `Error: ${e.message}`;
    }
}

// ---- Export ----

function exportScenario() {
    if (!currentScenario) return;

    const exportData = {
        version: 1,
        name: currentScenario.name,
        description: currentScenario.description || '',
        adapters: currentScenario.adapters.map(a => {
            const entry = {
                name: a.name,
                type: a.type,
                behavior_mode: a.behavior_mode,
                config: a.config,
            };
            if (a.credentials) entry.credentials = a.credentials;
            return entry;
        }),
    };

    document.getElementById('exportJson').value = JSON.stringify(exportData, null, 2);
    new bootstrap.Modal(document.getElementById('exportModal')).show();
}

function copyExportJson() {
    const text = document.getElementById('exportJson').value;
    navigator.clipboard.writeText(text).then(() => {
        const btn = document.getElementById('copyExportBtn');
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = 'Copy'; }, 2000);
    });
}

function downloadExportJson() {
    const text = document.getElementById('exportJson').value;
    const blob = new Blob([text], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = currentScenario.name.toLowerCase().replace(/\s+/g, '-') + '.json';
    a.click();
    URL.revokeObjectURL(url);
}

// ---- Fire (Sender Adapters) ----

let currentFireAdapterId = null;

async function fireAdapter(adapterId, adapterName) {
    currentFireAdapterId = adapterId;
    document.getElementById('fireAdapterName').textContent = adapterName;
    document.getElementById('fireResultContent').textContent = '(firing...)';

    const modal = new bootstrap.Modal(document.getElementById('fireResultModal'));
    modal.show();

    document.getElementById('fireAgainBtn').onclick = () => fireAdapter(adapterId, adapterName);

    await doFire(adapterId);
}

async function doFire(adapterId) {
    const content = document.getElementById('fireResultContent');
    try {
        const result = await api.triggerAdapter(currentScenario.id, adapterId);
        let text = '';
        if (result.error) {
            text += `Error: ${result.error}\n`;
        } else {
            text += `Status: ${result.status_code}\n`;
            text += `Sent to: ${result.sent_to}\n`;
            text += `Protocol: ${result.protocol}\n`;
            if (result.response_headers && Object.keys(result.response_headers).length > 0) {
                text += `\nResponse Headers:\n`;
                for (const [k, v] of Object.entries(result.response_headers)) {
                    text += `  ${k}: ${v}\n`;
                }
            }
            text += `\nResponse Body:\n${result.response_body || '(empty)'}`;
        }
        content.textContent = text;
    } catch (e) {
        content.textContent = `Failed: ${e.message}`;
    }
}

// ---- Clone (Use as Template) ----

async function cloneScenario() {
    const name = prompt('Name for the new scenario:', currentScenario.name + ' (copy)');
    if (name === null) return; // cancelled
    if (!name.trim()) {
        alert('Please enter a name.');
        return;
    }
    try {
        const newScenario = await api.cloneScenario(currentScenario.id, name.trim());
        window.location.href = `/scenario.html?id=${newScenario.id}`;
    } catch (error) {
        alert(`Failed to clone scenario: ${error.message}`);
    }
}
