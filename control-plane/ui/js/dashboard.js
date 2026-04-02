// Dashboard logic

document.addEventListener('DOMContentLoaded', async () => {
    await loadScenarios();
    await loadStartupLog();

    // Event listeners
    document.getElementById('createScenarioBtn').addEventListener('click', createScenario);
    document.getElementById('scenarioName').addEventListener('keypress', (e) => {
        if (e.key === 'Enter') createScenario();
    });
});

async function loadStartupLog() {
    try {
        const resp = await fetch('/api/system/log');
        const lines = await resp.json();
        if (!lines || lines.length === 0) return;
        const section = document.getElementById('startupLogSection');
        const log = document.getElementById('startupLog');
        log.textContent = lines.join('\n');
        section.style.display = '';
    } catch (e) {
        // non-fatal, just don't show the section
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
    try {
        const newScenario = await api.cloneScenario(scenarioId);
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

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
