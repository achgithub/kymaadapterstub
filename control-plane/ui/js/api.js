// API Client for Control Plane

class ControlPlaneAPI {
    constructor(baseURL = '/api') {
        this.baseURL = baseURL;
    }

    async request(method, endpoint, data = null) {
        const options = {
            method,
            headers: {
                'Content-Type': 'application/json',
            },
        };

        if (data) {
            options.body = JSON.stringify(data);
        }

        const response = await fetch(`${this.baseURL}${endpoint}`, options);

        if (!response.ok) {
            const error = await response.text();
            throw new Error(`${response.status}: ${error}`);
        }

        // Handle no content responses
        if (response.status === 204) {
            return null;
        }

        return await response.json();
    }

    // Scenario endpoints
    listScenarios() {
        return this.request('GET', '/scenarios');
    }

    createScenario(name) {
        return this.request('POST', '/scenarios', { name });
    }

    getScenario(id) {
        return this.request('GET', `/scenarios/${id}`);
    }

    updateScenario(id, name) {
        return this.request('PUT', `/scenarios/${id}`, { name });
    }

    deleteScenario(id) {
        return this.request('DELETE', `/scenarios/${id}`);
    }

    launchScenario(id) {
        return this.request('POST', `/scenarios/${id}/launch`);
    }

    stopScenario(id) {
        return this.request('POST', `/scenarios/${id}/stop`);
    }

    stopAdapter(scenarioId, adapterId) {
        return this.request('POST', `/scenarios/${scenarioId}/adapters/${adapterId}/stop`);
    }

    cleanupOrphanedResources() {
        return this.request('POST', '/cleanup');
    }

    cloneScenario(id) {
        return this.request('POST', `/scenarios/${id}/clone`);
    }

    // Adapter endpoints
    addAdapter(scenarioId, adapter) {
        return this.request('POST', `/scenarios/${scenarioId}/adapters`, adapter);
    }

    getAdapter(scenarioId, adapterId) {
        return this.request('GET', `/scenarios/${scenarioId}/adapters/${adapterId}`);
    }

    updateAdapter(scenarioId, adapterId, updates) {
        return this.request('PUT', `/scenarios/${scenarioId}/adapters/${adapterId}`, updates);
    }

    deleteAdapter(scenarioId, adapterId) {
        return this.request('DELETE', `/scenarios/${scenarioId}/adapters/${adapterId}`);
    }
}

// Create global API instance
const api = new ControlPlaneAPI();
