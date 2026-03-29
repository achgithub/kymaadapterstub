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

    // Show launch button only if stopped
    const launchBtn = document.getElementById('launchBtn');
    launchBtn.style.display = scenario.status === 'stopped' ? 'inline-block' : 'none';

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
                <td>
                    <button class="btn btn-sm btn-warning" onclick="editAdapter('${adapter.id}')">Edit</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteAdapter('${adapter.id}')">Delete</button>
                </td>
            </tr>
        `).join('');
    }

    currentScenario = scenario;
}

function updateConfigSections() {
    const type = document.getElementById('adapterType').value;
    document.getElementById('restConfig').style.display = type === 'REST' ? 'block' : 'none';
    document.getElementById('sftpConfig').style.display = type === 'SFTP' ? 'block' : 'none';
    document.getElementById('odataConfig').style.display = type === 'OData' ? 'block' : 'none';
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

    // Generate config edit form based on type
    let configHtml = '';
    if (adapter.type === 'REST') {
        configHtml = `
            <div class="mb-3">
                <label for="editStatusCode" class="form-label">Status Code</label>
                <input type="number" class="form-control" id="editStatusCode" value="${adapter.config.status_code || 200}">
            </div>
            <div class="mb-3">
                <label for="editBody" class="form-label">Response Body</label>
                <textarea class="form-control" id="editBody" rows="4">${escapeHtml(adapter.config.response_body || '')}</textarea>
            </div>
        `;
    } else if (adapter.type === 'SFTP') {
        configHtml = `
            <div class="mb-3">
                <label class="form-label">Files</label>
                <div id="editFilesList"></div>
            </div>
        `;
        // Would populate files here
    }

    document.getElementById('editConfigContainer').innerHTML = configHtml;
    new bootstrap.Modal(document.getElementById('editAdapterModal')).show();
}

async function updateAdapter() {
    const adapterId = document.getElementById('editAdapterId').value;
    const adapter = currentScenario.adapters.find(a => a.id === adapterId);
    if (!adapter) return;

    const behaviorMode = document.getElementById('editBehaviorMode').value;
    let config = adapter.config;

    // Update config based on type
    if (adapter.type === 'REST') {
        config.status_code = parseInt(document.getElementById('editStatusCode').value);
        config.response_body = document.getElementById('editBody').value;
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

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
