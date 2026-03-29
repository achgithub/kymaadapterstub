// Dashboard logic

document.addEventListener('DOMContentLoaded', async () => {
    await loadScenarios();

    // Event listeners
    document.getElementById('createScenarioBtn').addEventListener('click', createScenario);
    document.getElementById('scenarioName').addEventListener('keypress', (e) => {
        if (e.key === 'Enter') createScenario();
    });
});

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

    container.innerHTML = scenarios.map(scenario => `
        <div class="col-md-6 col-lg-4 mb-3">
            <div class="card scenario-card" onclick="goToScenario('${scenario.id}')">
                <div class="card-body">
                    <h5 class="scenario-card-title">${escapeHtml(scenario.name)}</h5>
                    <p class="scenario-card-meta">
                        ID: <code>${scenario.id}</code>
                    </p>
                    <div class="mb-3">
                        <span class="badge ${getStatusBadgeClass(scenario.status)}">
                            ${scenario.status.toUpperCase()}
                        </span>
                        <span class="badge bg-secondary">${scenario.adapters.length} adapters</span>
                    </div>
                    <p class="text-sm text-muted mb-0">
                        Created: ${new Date(scenario.created_at).toLocaleDateString()}
                    </p>
                </div>
                <div class="card-footer bg-light">
                    <button class="btn btn-sm btn-danger" onclick="deleteScenarioConfirm(event, '${scenario.id}')">
                        Delete
                    </button>
                </div>
            </div>
        </div>
    `).join('');
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
