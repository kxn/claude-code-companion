// Endpoints Modal JavaScript - 模态框相关功能

function showAddEndpointModal() {
    editingEndpointName = null;
    originalAuthValue = '';
    isAuthVisible = false;
    
    document.getElementById('endpointModalTitle').textContent = T('add_endpoint', '添加端点');
    document.getElementById('endpointForm').reset();
    document.getElementById('endpoint-enabled').checked = true;
    document.getElementById('endpoint-type').value = 'anthropic'; // Default to Anthropic
    document.getElementById('endpoint-tags').value = ''; // Clear tags field
    
    // Set endpoint type and switch path prefix display
    onEndpointTypeChange();
    
    // Reset auth visibility
    resetAuthVisibility();
    
    // Clear proxy configuration
    loadProxyConfig(null);
    
    // Clear model rewrite configuration
    loadModelRewriteConfig(null);
    
    // Clear default model
    document.getElementById('endpoint-default-model').value = '';

    // Clear model alias
    document.getElementById('endpoint-model-alias').value = '';
    
    
    // Clear header override configuration
    loadHeaderOverrideConfig(null);
    
    // Clear parameter override configuration
    loadParameterOverrideConfig(null);
    
    // Clear max tokens field name configuration
    document.getElementById('max-tokens-field-name').value = '';
    
    // Clear enhanced protection configuration
    document.getElementById('enhanced-protection-enabled').checked = false;
    
    // Check enhanced protection availability based on URL (will be checked again when URL changes)
    checkEnhancedProtectionAvailability();
    
    // Reset to basic configuration tab
    resetModalTabs();
    
    endpointModal.show();
}

function showEditEndpointModal(endpointName) {
    const endpoint = currentEndpoints.find(ep => ep.name === endpointName);
    if (!endpoint) {
        showAlert('端点未找到', 'danger');
        return;
    }

    editingEndpointName = endpointName;
    originalAuthValue = endpoint.auth_value;
    isAuthVisible = false;
    
    document.getElementById('endpointModalTitle').textContent = T('edit_endpoint', '编辑端点');
    
    // Populate form
    document.getElementById('endpoint-name').value = endpoint.name;
    document.getElementById('endpoint-url').value = endpoint.url;
    document.getElementById('endpoint-type').value = endpoint.endpoint_type || 'anthropic';
    document.getElementById('endpoint-path-prefix').value = endpoint.path_prefix || '';
    document.getElementById('endpoint-enabled').checked = endpoint.enabled;
    
    // Set endpoint type and switch path prefix display first
    onEndpointTypeChange();
    
    // Then set the auth type after the options are populated
    document.getElementById('endpoint-auth-type').value = endpoint.auth_type;
    
    // Set tags field
    const tagsValue = endpoint.tags && endpoint.tags.length > 0 ? endpoint.tags.join(', ') : '';
    document.getElementById('endpoint-tags').value = tagsValue;
    
    // Set auth value or OAuth config based on auth type
    if (endpoint.auth_type === 'oauth' && endpoint.oauth_config) {
        // Load OAuth configuration
        loadOAuthConfig(endpoint.oauth_config);
    } else {
        // Set auth value to asterisks
        document.getElementById('endpoint-auth-value').value = '*'.repeat(Math.min(endpoint.auth_value.length, 50));
        document.getElementById('endpoint-auth-value').type = 'password'; // Ensure it's password type
        document.getElementById('endpoint-auth-value').placeholder = '输入您的 API Key 或 Token';
        resetAuthVisibility();
    }
    
    // Update auth type display
    onAuthTypeChange();
    
    // Load proxy configuration
    loadProxyConfig(endpoint.proxy);
    
    // Load model rewrite configuration
    loadModelRewriteConfig(endpoint.model_rewrite);
    
    // Load default model after loading model rewrite config
    loadDefaultModel(endpoint.model_rewrite);

    // Load model alias
    document.getElementById('endpoint-model-alias').value = endpoint.model_alias || '';
    
    
    // Load header override configuration
    loadHeaderOverrideConfig(endpoint.header_overrides);
    
    // Load parameter override configuration
    loadParameterOverrideConfig(endpoint.parameter_overrides);
    
    // Load max tokens field name configuration
    const maxTokensFieldName = endpoint.max_tokens_field_name || '';
    document.getElementById('max-tokens-field-name').value = maxTokensFieldName;
    
    // Load enhanced protection configuration
    const enhancedProtection = endpoint.enhanced_protection || false;
    document.getElementById('enhanced-protection-enabled').checked = enhancedProtection;
    
    // Check enhanced protection availability based on URL
    checkEnhancedProtectionAvailability();
    
    // Reset to basic configuration tab
    resetModalTabs();
    
    endpointModal.show();
}

// Reset modal tabs to basic configuration
function resetModalTabs() {
    // Reset tab state
    const basicTab = document.getElementById('basic-tab');
    const advancedTab = document.getElementById('advanced-tab');
    const advanced2Tab = document.getElementById('advanced2-tab');
    const basicPane = document.getElementById('basic-tab-pane');
    const advancedPane = document.getElementById('advanced-tab-pane');
    const advanced2Pane = document.getElementById('advanced2-tab-pane');
    
    // Activate basic configuration tab
    basicTab.classList.add('active');
    basicTab.setAttribute('aria-selected', 'true');
    basicPane.classList.add('show', 'active');
    
    // Deactivate advanced configuration tabs
    advancedTab.classList.remove('active');
    advancedTab.setAttribute('aria-selected', 'false');
    advancedPane.classList.remove('show', 'active');
    
    if (advanced2Tab && advanced2Pane) {
        advanced2Tab.classList.remove('active');
        advanced2Tab.setAttribute('aria-selected', 'false');
        advanced2Pane.classList.remove('show', 'active');
    }
}

function saveEndpoint() {
    const form = document.getElementById('endpointForm');
    if (!form.checkValidity()) {
        form.reportValidity();
        return;
    }

    const authType = document.getElementById('endpoint-auth-type').value;
    
    // Get auth value or OAuth config based on auth type
    let authValue = '';
    let oauthConfig = null;
    
    if (authType === 'oauth') {
        // Collect OAuth configuration
        const scopesInput = document.getElementById('oauth-scopes').value.trim();
        const scopes = scopesInput ? scopesInput.split(',').map(s => s.trim()).filter(s => s) : [];
        
        oauthConfig = {
            access_token: document.getElementById('oauth-access-token').value,
            refresh_token: document.getElementById('oauth-refresh-token').value,
            expires_at: parseInt(document.getElementById('oauth-expires-at').value),
            token_url: document.getElementById('oauth-token-url').value,
            client_id: document.getElementById('oauth-client-id').value || '',
            scopes: scopes,
            auto_refresh: document.getElementById('oauth-auto-refresh').checked
        };
        
        // Remove empty optional fields
        if (!oauthConfig.client_id) delete oauthConfig.client_id;
        if (oauthConfig.scopes.length === 0) delete oauthConfig.scopes;
    } else {
        // Get regular auth value
        authValue = document.getElementById('endpoint-auth-value').value;
        if (!isAuthVisible && originalAuthValue && authValue.startsWith('*')) {
            // If showing asterisks and has original value, use original value
            authValue = originalAuthValue;
        }
    }

    // Model alias
    const modelAlias = document.getElementById('endpoint-model-alias').value.trim();

    // Parse tags field
    const tagsInput = document.getElementById('endpoint-tags').value.trim();
    const tags = tagsInput ? tagsInput.split(',').map(tag => tag.trim()).filter(tag => tag) : [];
    if (modelAlias && !tags.includes(modelAlias)) {
        tags.push(modelAlias);
    }

    const data = {
        name: document.getElementById('endpoint-name').value,
        url: document.getElementById('endpoint-url').value,
        endpoint_type: document.getElementById('endpoint-type').value,
        path_prefix: document.getElementById('endpoint-path-prefix').value || '', // PathPrefix can be empty
        auth_type: authType,
        auth_value: authValue,
        enabled: document.getElementById('endpoint-enabled').checked,
        tags: tags,
        model_alias: modelAlias,
        max_tokens_field_name: document.getElementById('max-tokens-field-name').value || '', // New: max tokens field name
        proxy: collectProxyData(), // New: collect proxy configuration
        header_overrides: collectHeaderOverrideData(), // New: collect header override configuration
        parameter_overrides: collectParameterOverrideData(), // New: collect parameter override configuration
        enhanced_protection: document.getElementById('enhanced-protection-enabled').checked // New: enhanced protection for official accounts
    };
    
    // Add OAuth config if present
    if (oauthConfig) {
        data.oauth_config = oauthConfig;
    }

    const isEditing = editingEndpointName !== null;
    const url = isEditing 
        ? `/admin/api/endpoints/${encodeURIComponent(editingEndpointName)}` 
        : '/admin/api/endpoints';
    const method = isEditing ? 'PUT' : 'POST';

    apiRequest(url, {
        method: method,
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(data)
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showAlert(data.error, 'danger');
        } else {
            // After successful save, if there's model rewrite configuration, save it too
            const modelRewriteConfig = collectModelRewriteData();
            const endpointName = document.getElementById('endpoint-name').value;
            
            const saveModelRewrite = modelRewriteConfig 
                ? saveModelRewriteConfig(endpointName, modelRewriteConfig)
                : Promise.resolve();
            
            saveModelRewrite
                .then(() => {
                    endpointModal.hide();
                    showAlert(data.message, 'success');
                    loadEndpoints(); // Reload data instead of refreshing page
                })
                .catch(error => {
                    console.error('Failed to save model rewrite config:', error);
                    showAlert(T('endpoint_save_success_rewrite_failed', '端点保存成功，但模型重写配置保存失败') + ': ' + error.message, 'warning');
                    endpointModal.hide();
                    loadEndpoints();
                });
        }
    })
    .catch(error => {
        console.error('Failed to save endpoint:', error);
        showAlert(T('failed_to_save_endpoint', 'Failed to save endpoint'), 'danger');
    });
}

function deleteEndpoint(endpointName) {
    if (!confirm(T('confirm_delete_endpoint', '确定要删除端点 "{0}" 吗？').replace('{0}', endpointName))) {
        return;
    }

    apiRequest(`/admin/api/endpoints/${encodeURIComponent(endpointName)}`, {
        method: 'DELETE'
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showAlert(data.error, 'danger');
        } else {
            showAlert(data.message, 'success');
            loadEndpoints(); // Reload data instead of refreshing page
        }
    })
    .catch(error => {
        console.error('Failed to delete endpoint:', error);
        showAlert(T('failed_to_delete_endpoint', 'Failed to delete endpoint'), 'danger');
    });
}

function copyEndpoint(endpointName) {
    if (!confirm(T('confirm_copy_endpoint', '确定要复制端点 "{0}" 吗？').replace('{0}', endpointName))) {
        return;
    }

    apiRequest(`/admin/api/endpoints/${encodeURIComponent(endpointName)}/copy`, {
        method: 'POST'
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showAlert(data.error, 'danger');
        } else {
            showAlert(data.message, 'success');
            loadEndpoints(); // Reload data to show newly copied endpoint
        }
    })
    .catch(error => {
        console.error('Failed to copy endpoint:', error);
        showAlert(T('failed_to_copy_endpoint', 'Failed to copy endpoint'), 'danger');
    });
}

function toggleEndpointEnabled(endpointName, currentEnabled) {
    const newEnabled = !currentEnabled;
    const actionText = newEnabled ? '启用' : '禁用';
    
    // 获取当前端点的状态信息
    const currentEndpoint = currentEndpoints.find(ep => ep.name === endpointName);
    const currentStatus = currentEndpoint ? currentEndpoint.status : 'unknown';
    
    apiRequest(`/admin/api/endpoints/${encodeURIComponent(endpointName)}/toggle`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({
            enabled: newEnabled
        })
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showAlert(data.error, 'danger');
        } else {
            showAlert(T('endpoint_action_success', '端点 "{0}" 已{1}').replace('{0}', endpointName).replace('{1}', actionText), 'success');
            // 更新按钮状态而不重新加载整个表格
            updateEndpointToggleButton(endpointName, newEnabled);
            // 更新启用状态显示
            updateEndpointEnabledBadge(endpointName, newEnabled);
            // 更新状态badge显示
            updateEndpointStatusBadge(endpointName, newEnabled, currentStatus);
        }
    })
    .catch(error => {
        console.error('Failed to toggle endpoint:', error);
        showAlert(T('endpoint_action_failed', '{0}端点失败').replace('{0}', actionText), 'danger');
    });
}

function resetEndpointStatus(endpointName) {
    if (!confirm(T('confirm_reset_endpoint_status', '确认要重置端点 "{0}" 的状态吗？这将清除失败记录并将状态重置为正常。').replace('{0}', endpointName))) {
        return;
    }

    apiRequest(`/admin/api/endpoints/${encodeURIComponent(endpointName)}/reset-status`, {
        method: 'POST'
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showAlert(data.error, 'danger');
        } else {
            showAlert(T('endpoint_status_reset_success', '端点 "{0}" 状态已重置为正常').replace('{0}', endpointName), 'success');
            // 刷新端点状态显示
            refreshEndpointStatus();
        }
    })
    .catch(error => {
        console.error('Failed to reset endpoint status:', error);
        showAlert(T('reset_endpoint_status_failed', '重置端点状态失败'), 'danger');
    });
}

function reorderEndpoints() {
    // Get special endpoint order
    const specialRows = document.querySelectorAll('#special-endpoint-list tr');
    const specialOrderedNames = Array.from(specialRows).map(row => row.dataset.endpointName);
    
    // Get general endpoint order
    const generalRows = document.querySelectorAll('#general-endpoint-list tr');
    const generalOrderedNames = Array.from(generalRows).map(row => row.dataset.endpointName);
    
    // Merge order: special endpoints first, general endpoints later
    const orderedNames = [...specialOrderedNames, ...generalOrderedNames];
    
    apiRequest('/admin/api/endpoints/reorder', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({
            ordered_names: orderedNames
        })
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showAlert(data.error, 'danger');
            loadEndpoints(); // Reload to restore order
        } else {
            showAlert(data.message, 'success');
            // Update priority display, no need to reload entire table
            let priorityIndex = 1;
            
            // Update special endpoint priorities
            specialRows.forEach((row) => {
                const priorityBadge = row.querySelector('.priority-badge');
                if (priorityBadge) {
                    priorityBadge.textContent = priorityIndex++;
                }
            });
            
            // Update general endpoint priorities
            generalRows.forEach((row) => {
                const priorityBadge = row.querySelector('.priority-badge');
                if (priorityBadge) {
                    priorityBadge.textContent = priorityIndex++;
                }
            });
        }
    })
    .catch(error => {
        console.error('Failed to reorder endpoints:', error);
        showAlert('Failed to reorder endpoints', 'danger');
        loadEndpoints(); // Reload to restore order
    });
}

// Check if URL is api.anthropic.com and enable/disable enhanced protection accordingly
function checkEnhancedProtectionAvailability() {
    const urlInput = document.getElementById('endpoint-url');
    const enhancedProtectionCheckbox = document.getElementById('enhanced-protection-enabled');
    
    if (!urlInput || !enhancedProtectionCheckbox) {
        return;
    }
    
    const url = urlInput.value.toLowerCase().trim();
    const isAnthropicOfficial = url.includes('api.anthropic.com');
    
    if (isAnthropicOfficial) {
        // Enable enhanced protection option for api.anthropic.com
        enhancedProtectionCheckbox.disabled = false;
        enhancedProtectionCheckbox.parentElement.parentElement.style.opacity = '1';
    } else {
        // Disable enhanced protection option for non-anthropic endpoints
        enhancedProtectionCheckbox.disabled = true;
        enhancedProtectionCheckbox.checked = false;
        enhancedProtectionCheckbox.parentElement.parentElement.style.opacity = '0.5';
    }
}

// Add event listener for URL input changes
document.addEventListener('DOMContentLoaded', function() {
    const urlInput = document.getElementById('endpoint-url');
    if (urlInput) {
        // Add event listener for input events (real-time typing)
        urlInput.addEventListener('input', checkEnhancedProtectionAvailability);
        // Add event listener for change events (when user leaves the field)
        urlInput.addEventListener('change', checkEnhancedProtectionAvailability);
        // Add event listener for blur events (when field loses focus)
        urlInput.addEventListener('blur', checkEnhancedProtectionAvailability);
    }
});