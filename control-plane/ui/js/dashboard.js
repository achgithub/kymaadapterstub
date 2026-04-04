// Dashboard logic

document.addEventListener('DOMContentLoaded', async () => {
    await loadScenarios();
    await loadSystemLog();

    // Auto-refresh system log every 30 seconds
    setInterval(loadSystemLog, 30000);

    // Event listeners
    document.getElementById('createScenarioBtn').addEventListener('click', createScenario);
    document.getElementById('scenarioName').addEventListener('keypress', (e) => {
        if (e.key === 'Enter') createScenario();
    });
    document.getElementById('logTailInput').addEventListener('change', loadSystemLog);
});

async function loadSystemLog() {
    const section = document.getElementById('systemLogSection');
    const log = document.getElementById('systemLog');
    const tail = document.getElementById('logTailInput')?.value || 50;

    try {
        const resp = await fetch(`/api/system/log?tail=${tail}`);
        if (!resp.ok) {
            log.textContent = `Error fetching log: HTTP ${resp.status}`;
            section.style.display = '';
            return;
        }
        const lines = await resp.json();
        section.style.display = '';
        log.textContent = (lines && lines.length > 0) ? lines.join('\n') : '(no log entries yet)';
        log.scrollTop = log.scrollHeight;
    } catch (e) {
        log.textContent = `Error: ${e.message}`;
        section.style.display = '';
    }
}

async function loadScenarios() {
    try {
        const scenarios = await api.listScenarios();
        displayScenarios(scenarios);
    } catch (error) {
        console.error('Failed to load scenarios:', error);
        document.getElementById('scenariosList').innerHTML = `
            <div class="col-12">
                <div class="alert alert-danger">Failed to load scenarios: ${error.message}</div>
            </div>
        `;
    }
}

function displayScenarios(scenarios) {
    const container = document.getElementById('scenariosList');

    if (!scenarios || scenarios.length === 0) {
        container.innerHTML = `
            <div class="col-12">
                <div class="alert alert-info">
                    No scenarios yet. <button class="btn btn-primary btn-sm" data-bs-toggle="modal" data-bs-target="#newScenarioModal">Create one</button>
                </div>
            </div>
        `;
        return;
    }

    container.innerHTML = scenarios.map(scenario => {
        const isGithub = scenario.source === 'github';
        const footer = isGithub
            ? `<button class="btn btn-sm btn-outline-primary" onclick="useAsTemplate(event, '${scenario.id}')">Use as Template</button>`
            : `<button class="btn btn-sm btn-danger" onclick="deleteScenarioConfirm(event, '${scenario.id}')">Delete</button>`;
        const lockBadge = isGithub ? `<span class="badge bg-info ms-1">Example</span>` : '';
        const description = scenario.description
            ? `<p class="text-muted small mb-2">${escapeHtml(scenario.description)}</p>` : '';

        return `
        <div class="col-md-6 col-lg-4 mb-3">
            <div class="card scenario-card" onclick="goToScenario('${scenario.id}')">
                <div class="card-body">
                    <h5 class="scenario-card-title">${escapeHtml(scenario.name)}${lockBadge}</h5>
                    ${description}
                    <div class="mb-3">
                        <span class="badge ${getStatusBadgeClass(scenario.status)}">
                            ${scenario.status.toUpperCase()}
                        </span>
                        <span class="badge bg-secondary">${scenario.adapters.length} adapters</span>
                    </div>
                </div>
                <div class="card-footer bg-light">
                    ${footer}
                </div>
            </div>
        </div>`;
    }).join('');
}

async function createScenario() {
    const name = document.getElementById('scenarioName').value.trim();

    if (!name) {
        alert('Please enter a scenario name');
        return;
    }

    try {
        const scenario = await api.createScenario(name);
        document.getElementById('scenarioName').value = '';
        bootstrap.Modal.getInstance(document.getElementById('newScenarioModal')).hide();
        goToScenario(scenario.id);
    } catch (error) {
        alert(`Failed to create scenario: ${error.message}`);
    }
}

async function deleteScenarioConfirm(event, scenarioId) {
    event.stopPropagation();

    if (!confirm('Are you sure you want to delete this scenario? This will also delete all its adapters.')) {
        return;
    }

    try {
        await api.deleteScenario(scenarioId);
        await loadScenarios();
    } catch (error) {
        alert(`Failed to delete scenario: ${error.message}`);
    }
}

async function cleanupOrphanedResources() {
    if (!confirm('Delete all adapter pods, services and APIRules from Kubernetes? This clears any orphaned resources left after a control plane restart.')) {
        return;
    }

    const btn = document.getElementById('cleanupBtn');
    btn.disabled = true;
    btn.textContent = 'Cleaning up...';

    try {
        await api.cleanupOrphanedResources();
        alert('Cleanup complete.');
    } catch (error) {
        alert(`Cleanup failed: ${error.message}`);
    } finally {
        btn.disabled = false;
        btn.textContent = '🧹 Cleanup Orphaned Pods';
    }
}

async function useAsTemplate(event, scenarioId) {
    event.stopPropagation();
    const scenario = await api.getScenario(scenarioId).catch(() => null);
    const defaultName = scenario ? scenario.name + ' (copy)' : 'New Scenario';
    const name = prompt('Name for the new scenario:', defaultName);
    if (name === null) return; // cancelled
    if (!name.trim()) {
        alert('Please enter a name.');
        return;
    }
    try {
        const newScenario = await api.cloneScenario(scenarioId, name.trim());
        goToScenario(newScenario.id);
    } catch (error) {
        alert(`Failed to clone scenario: ${error.message}`);
    }
}

function goToScenario(scenarioId) {
    window.location.href = `/scenario.html?id=${scenarioId}`;
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

// ---- Import / Export ----

async function openImportExport() {
    // Populate export dropdown with user-owned scenarios only
    const scenarios = await api.listScenarios().catch(() => []);
    const select = document.getElementById('exportScenarioSelect');
    select.innerHTML = '<option value="">-- Select a scenario --</option>';
    scenarios
        .filter(s => s.source !== 'github')
        .forEach(s => {
            const opt = document.createElement('option');
            opt.value = s.id;
            opt.textContent = s.name;
            select.appendChild(opt);
        });

    // Reset state
    document.getElementById('exportJsonText').value = '';
    document.getElementById('importJsonText').value = '';
    document.getElementById('importFileInput').value = '';
    document.getElementById('copyExportBtn').disabled = true;
    document.getElementById('downloadExportBtn').disabled = true;
    document.getElementById('importBtn').style.display = 'none';

    // Switch Import button visibility with tabs
    const importTabBtn = document.getElementById('importTabBtn');
    const exportTabBtn = document.getElementById('exportTabBtn');
    importTabBtn.addEventListener('shown.bs.tab', () => {
        document.getElementById('importBtn').style.display = 'inline-block';
    }, { once: false });
    exportTabBtn.addEventListener('shown.bs.tab', () => {
        document.getElementById('importBtn').style.display = 'none';
    }, { once: false });

    // Reset to export tab
    new bootstrap.Tab(exportTabBtn).show();

    new bootstrap.Modal(document.getElementById('importExportModal')).show();
}

async function loadExportPreview() {
    const scenarioId = document.getElementById('exportScenarioSelect').value;
    const textarea = document.getElementById('exportJsonText');
    const copyBtn = document.getElementById('copyExportBtn');
    const dlBtn = document.getElementById('downloadExportBtn');

    if (!scenarioId) {
        textarea.value = '';
        copyBtn.disabled = true;
        dlBtn.disabled = true;
        return;
    }

    try {
        const scenario = await api.getScenario(scenarioId);
        const exportData = {
            version: 1,
            name: scenario.name,
            description: scenario.description || '',
            adapters: scenario.adapters.map(a => {
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
        textarea.value = JSON.stringify(exportData, null, 2);
        copyBtn.disabled = false;
        dlBtn.disabled = false;
    } catch (e) {
        textarea.value = `Error loading scenario: ${e.message}`;
        copyBtn.disabled = true;
        dlBtn.disabled = true;
    }
}

function copyExportJson() {
    const text = document.getElementById('exportJsonText').value;
    navigator.clipboard.writeText(text).then(() => {
        const btn = document.getElementById('copyExportBtn');
        const orig = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = orig; }, 2000);
    });
}

function downloadExportJson() {
    const text = document.getElementById('exportJsonText').value;
    if (!text) return;
    const select = document.getElementById('exportScenarioSelect');
    const name = select.options[select.selectedIndex]?.text || 'scenario';
    const blob = new Blob([text], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = name.toLowerCase().replace(/\s+/g, '-') + '.json';
    a.click();
    URL.revokeObjectURL(url);
}

function handleImportFile(event) {
    const file = event.target.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = e => {
        document.getElementById('importJsonText').value = e.target.result;
    };
    reader.readAsText(file);
}

async function importScenario() {
    const text = document.getElementById('importJsonText').value.trim();
    if (!text) {
        alert('Please paste or upload a scenario JSON first.');
        return;
    }

    let data;
    try {
        data = JSON.parse(text);
    } catch (e) {
        alert('Invalid JSON: ' + e.message);
        return;
    }

    if (!data.name || !Array.isArray(data.adapters)) {
        alert('Invalid scenario format — JSON must have "name" and "adapters" fields.');
        return;
    }

    // Check for name collision and prompt for a new name if needed
    const existing = await api.listScenarios().catch(() => []);
    let importName = data.name;
    if (existing.some(s => s.name === importName)) {
        const suggested = importName + ' (imported)';
        importName = prompt(`A scenario named "${importName}" already exists.\nEnter a new name:`, suggested);
        if (importName === null) return; // cancelled
        importName = importName.trim();
        if (!importName) {
            alert('Please enter a name.');
            return;
        }
    }

    const btn = document.getElementById('importBtn');
    btn.disabled = true;
    btn.textContent = 'Importing...';

    try {
        const scenario = await api.createScenario(importName, data.description || '');
        for (const adapter of data.adapters) {
            await api.addAdapter(scenario.id, {
                name: adapter.name,
                type: adapter.type,
                behavior_mode: adapter.behavior_mode || 'success',
                config: adapter.config || {},
                credentials: adapter.credentials || null,
            });
        }
        bootstrap.Modal.getInstance(document.getElementById('importExportModal')).hide();
        goToScenario(scenario.id);
    } catch (error) {
        alert(`Import failed: ${error.message}`);
        btn.disabled = false;
        btn.textContent = 'Import Scenario';
    }
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
