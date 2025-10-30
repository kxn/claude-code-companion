// Endpoints Advanced JavaScript - 高级功能

// ===== Modal Functions =====

// Proxy configuration enable/disable toggle
document.getElementById('proxy-enabled').addEventListener('change', function() {
    const proxyConfigDiv = document.getElementById('proxy-config');
    this.checked ? StyleUtils.show(proxyConfigDiv) : StyleUtils.hide(proxyConfigDiv);
});

// Model rewrite enable/disable toggle
document.getElementById('model-rewrite-enabled').addEventListener('change', function() {
    const rulesDiv = document.getElementById('model-rewrite-rules');
    this.checked ? StyleUtils.show(rulesDiv) : StyleUtils.hide(rulesDiv);
    
    // If disabling model rewrite, check if we should clear default model rules
    if (!this.checked) {
        clearDefaultModelRulesIfApplicable();
    }
    
    // Update default model state when model rewrite toggle changes
    updateDefaultModelState();
});


// Add rewrite rule
function addRewriteRule(sourcePattern = '', targetModel = '') {
    const rulesList = document.getElementById('rewrite-rules-list');
    const ruleIndex = rulesList.children.length;
    
    const ruleDiv = document.createElement('div');
    ruleDiv.className = 'row mb-2 rewrite-rule';
    
    // 检查翻译系统是否可用
    const selectPresetText = typeof T === 'function' ? T('select_preset_model', '选择预设模型') : '选择预设模型';
    const customWildcardText = typeof T === 'function' ? T('custom_wildcard', '自定义通配符') : '自定义通配符';
    const wildcardPatternText = typeof T === 'function' ? T('wildcard_pattern', '通配符模式') : '通配符模式';
    const targetModelPlaceholderText = typeof T === 'function' ? T('target_model_placeholder', '目标模型 (如: deepseek-chat)') : '目标模型 (如: deepseek-chat)';
    const testRuleText = typeof T === 'function' ? T('test_rule', '测试规则') : '测试规则';
    
    ruleDiv.innerHTML = `
        <div class="col-5">
            <select class="form-select source-model-select" onchange="updateSourcePattern(${ruleIndex})">
                <option value="">${selectPresetText}</option>
                <option value="claude-*haiku*">Haiku 系列</option>
                <option value="claude-*sonnet*">Sonnet 系列</option>
                <option value="claude-*opus*">Opus 系列</option>
                <option value="claude-*">所有 Claude</option>
                <option value="custom">${customWildcardText}</option>
            </select>
            <input type="text" class="form-control mt-1 source-pattern-input" 
                   placeholder="${wildcardPatternText}" value="${escapeHtml(sourcePattern)}" readonly>
        </div>
        <div class="col-5">
            <input type="text" class="form-control target-model-input" 
                   placeholder="${targetModelPlaceholderText}" value="${escapeHtml(targetModel)}" 
                   oninput="onRewriteRuleTargetChange()">
        </div>
        <div class="col-2">
            <button type="button" class="btn btn-outline-danger btn-sm" onclick="removeRewriteRule(this)">
                <i class="fas fa-trash"></i>
            </button>
            <button type="button" class="btn btn-outline-info btn-sm mt-1" onclick="testRewriteRule(${ruleIndex})" title="${testRuleText}">
                <i class="fas fa-play"></i>
            </button>
        </div>
    `;
    
    rulesList.appendChild(ruleDiv);
    
    // Update default model state when rules change
    updateDefaultModelState();
}

// Clear default model rules when disabling model rewrite
function clearDefaultModelRulesIfApplicable() {
    const rules = collectCurrentRewriteRules();
    
    // If there's exactly one rule with pattern "*", remove it
    if (rules.length === 1 && rules[0].source_pattern === '*') {
        document.getElementById('rewrite-rules-list').innerHTML = '';
        // Clear default model value as well
        document.getElementById('endpoint-default-model').value = '';
    }
}

// Handle target model changes in rewrite rules
function onRewriteRuleTargetChange() {
    // Update default model state when target model changes
    updateDefaultModelState();
}

// Update source pattern input
function updateSourcePattern(ruleIndex) {
    const ruleDiv = document.querySelectorAll('.rewrite-rule')[ruleIndex];
    const select = ruleDiv.querySelector('.source-model-select');
    const input = ruleDiv.querySelector('.source-pattern-input');
    
    if (select.value === 'custom') {
        input.readOnly = false;
        input.focus();
    } else {
        input.readOnly = true;
        input.value = select.value;
    }
    
    // Update default model state when pattern changes
    updateDefaultModelState();
}

// Remove rewrite rule
function removeRewriteRule(button) {
    button.closest('.rewrite-rule').remove();
    // Update default model state when rules change
    updateDefaultModelState();
}

// Test rewrite rule
function testRewriteRule(ruleIndex) {
    const promptText = typeof T === 'function' ? T('enter_test_model_name', '请输入要测试的模型名称:') : '请输入要测试的模型名称:';
    const testModel = prompt(promptText, 'claude-3-haiku-20240307');
    if (!testModel) return;

    if (!editingEndpointName) {
        const alertText = typeof T === 'function' ? T('save_endpoint_before_test', '请先保存端点后再测试规则') : '请先保存端点后再测试规则';
        alert(alertText);
        return;
    }

    apiRequest(`/admin/api/endpoints/${encodeURIComponent(editingEndpointName)}/test-model-rewrite`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ test_model: testModel })
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            const errorText = typeof T === 'function' ? T('test_failed_error', '测试失败') : '测试失败';
            alert(errorText + `: ${data.error}`);
        } else {
            const successText = typeof T === 'function' ? 
                T('rewrite_success_message', '✅ 重写生效!\\n原模型: {0}\\n重写为: {1}\\n匹配规则: {2}').replace('{0}', data.original_model).replace('{1}', data.rewritten_model).replace('{2}', data.matched_rule) : 
                `✅ 重写生效!\n原模型: ${data.original_model}\n重写为: ${data.rewritten_model}\n匹配规则: ${data.matched_rule}`;
            const noRewriteText = typeof T === 'function' ? 
                T('no_rewrite_message', '❌ 无重写\\n模型: {0}\\n未匹配任何规则').replace('{0}', data.original_model) : 
                `❌ 无重写\n模型: ${data.original_model}\n未匹配任何规则`;
            const message = data.rewrite_applied ? successText : noRewriteText;
            alert(message);
        }
    })
    .catch(error => {
        console.error('Test failed:', error);
        const networkErrorText = typeof T === 'function' ? T('test_failed_network', '测试失败，请检查网络连接') : '测试失败，请检查网络连接';
        alert(networkErrorText);
    });
}

// Collect proxy configuration data
function collectProxyData() {
    const enabled = document.getElementById('proxy-enabled').checked;
    if (!enabled) {
        return null;
    }

    const type = document.getElementById('proxy-type').value;
    const address = document.getElementById('proxy-address').value.trim();
    const username = document.getElementById('proxy-username').value.trim();
    const password = document.getElementById('proxy-password').value.trim();
    
    if (!address) {
        return null; // Don't save proxy config if address is empty
    }

    const proxyConfig = {
        type: type,
        address: address
    };

    // Only add auth info if both username and password are not empty
    if (username && password) {
        proxyConfig.username = username;
        proxyConfig.password = password;
    }

    return proxyConfig;
}

// Load proxy configuration to form
function loadProxyConfig(config) {
    const checkbox = document.getElementById('proxy-enabled');
    const configDiv = document.getElementById('proxy-config');
    
    if (config) {
        checkbox.checked = true;
        StyleUtils.show(configDiv);
        
        document.getElementById('proxy-type').value = config.type || 'http';
        document.getElementById('proxy-address').value = config.address || '';
        document.getElementById('proxy-username').value = config.username || '';
        document.getElementById('proxy-password').value = config.password || '';
    } else {
        checkbox.checked = false;
        StyleUtils.hide(configDiv);
        
        // Reset form fields
        document.getElementById('proxy-type').value = 'http';
        document.getElementById('proxy-address').value = '';
        document.getElementById('proxy-username').value = '';
        document.getElementById('proxy-password').value = '';
    }
}

// Collect model rewrite configuration data
function collectModelRewriteData() {
    const enabled = document.getElementById('model-rewrite-enabled').checked;
    if (!enabled) {
        return null;
    }

    const rules = [];
    document.querySelectorAll('.rewrite-rule').forEach(ruleDiv => {
        const sourcePattern = ruleDiv.querySelector('.source-pattern-input').value.trim();
        const targetModel = ruleDiv.querySelector('.target-model-input').value.trim();
        
        if (sourcePattern && targetModel) {
            rules.push({
                source_pattern: sourcePattern,
                target_model: targetModel
            });
        }
    });

    return rules.length > 0 ? { enabled: true, rules: rules } : null;
}

// Load model rewrite configuration to form
function loadModelRewriteConfig(config) {
    const checkbox = document.getElementById('model-rewrite-enabled');
    const rulesDiv = document.getElementById('model-rewrite-rules');
    const rulesList = document.getElementById('rewrite-rules-list');
    
    // Clear existing rules
    rulesList.innerHTML = '';
    
    if (config && config.enabled && config.rules) {
        checkbox.checked = true;
        StyleUtils.show(rulesDiv);
        
        config.rules.forEach(rule => {
            addRewriteRule(rule.source_pattern, rule.target_model);
        });
    } else {
        checkbox.checked = false;
        StyleUtils.hide(rulesDiv);
    }
    
    // Update default model state after loading model rewrite config
    updateDefaultModelState();
}

// Save model rewrite configuration
function saveModelRewriteConfig(endpointName, config) {
    if (!config) return Promise.resolve();

    return apiRequest(`/admin/api/endpoints/${encodeURIComponent(endpointName)}/model-rewrite`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config)
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            throw new Error(data.error);
        }
        return data;
    });
}


// ===== Default Model Functions =====

// Load default model from model rewrite configuration
function loadDefaultModel(modelRewriteConfig) {
    const defaultModelInput = document.getElementById('endpoint-default-model');
    
    if (modelRewriteConfig && modelRewriteConfig.enabled && modelRewriteConfig.rules) {
        // Check if there's exactly one rule with pattern "*"
        if (modelRewriteConfig.rules.length === 1 && modelRewriteConfig.rules[0].source_pattern === '*') {
            defaultModelInput.value = modelRewriteConfig.rules[0].target_model;
        } else {
            defaultModelInput.value = '';
        }
    } else {
        defaultModelInput.value = '';
    }
    
    updateDefaultModelState();
}

// Update default model state based on model rewrite configuration
function updateDefaultModelState() {
    const defaultModelInput = document.getElementById('endpoint-default-model');
    const defaultModelHint = document.getElementById('default-model-hint');
    const modelRewriteEnabled = document.getElementById('model-rewrite-enabled').checked;
    
    if (!modelRewriteEnabled) {
        // Model rewrite disabled - default model can be edited
        defaultModelInput.disabled = false;
        defaultModelInput.title = '';
        StyleUtils.hide(defaultModelHint);
    } else {
        // Model rewrite enabled - check rules
        const rules = collectCurrentRewriteRules();
        
        if (rules.length === 0) {
            // No rules - default model can be edited
            defaultModelInput.disabled = false;
            defaultModelInput.title = '';
            StyleUtils.hide(defaultModelHint);
        } else if (rules.length === 1 && rules[0].source_pattern === '*') {
            // Single "*" rule - sync with default model
            defaultModelInput.disabled = false;
            defaultModelInput.title = '';
            defaultModelInput.value = rules[0].target_model;
            StyleUtils.hide(defaultModelHint);
        } else {
            // Multiple rules or non-"*" rules - disable default model
            defaultModelInput.disabled = true;
            const titleText = typeof T === 'function' ? T('model_rewrite_incompatible_settings', 'Model Rewrite中有和默认模型不兼容的设置') : 'Model Rewrite中有和默认模型不兼容的设置';
            defaultModelInput.title = titleText;
            StyleUtils.show(defaultModelHint);
        }
    }
}

// Collect current rewrite rules from the form
function collectCurrentRewriteRules() {
    const rules = [];
    document.querySelectorAll('.rewrite-rule').forEach(ruleDiv => {
        const sourcePattern = ruleDiv.querySelector('.source-pattern-input').value.trim();
        const targetModel = ruleDiv.querySelector('.target-model-input').value.trim();
        
        if (sourcePattern && targetModel) {
            rules.push({
                source_pattern: sourcePattern,
                target_model: targetModel
            });
        }
    });
    return rules;
}

// Handle default model changes
function onDefaultModelChange() {
    const defaultModelInput = document.getElementById('endpoint-default-model');
    const modelRewriteEnabled = document.getElementById('model-rewrite-enabled').checked;
    const defaultModel = defaultModelInput.value.trim();
    
    if (!modelRewriteEnabled && defaultModel) {
        // Enable model rewrite and set single "*" rule
        document.getElementById('model-rewrite-enabled').checked = true;
        StyleUtils.show(document.getElementById('model-rewrite-rules'));
        
        // Clear existing rules and add new "*" rule
        document.getElementById('rewrite-rules-list').innerHTML = '';
        addRewriteRule('*', defaultModel);
    } else if (modelRewriteEnabled) {
        const rules = collectCurrentRewriteRules();
        if (rules.length === 1 && rules[0].source_pattern === '*') {
            // Update the single "*" rule
            const targetInput = document.querySelector('.rewrite-rule .target-model-input');
            if (targetInput) {
                targetInput.value = defaultModel;
            }
        }
    }
    
    updateDefaultModelState();
}

// Add event listener for default model input
document.addEventListener('DOMContentLoaded', function() {
    const defaultModelInput = document.getElementById('endpoint-default-model');
    if (defaultModelInput) {
        defaultModelInput.addEventListener('input', onDefaultModelChange);
        defaultModelInput.addEventListener('blur', onDefaultModelChange);
    }
});

// ===== HTTP Header Override Functions =====

// Header override enable/disable toggle
document.getElementById('header-override-enabled').addEventListener('change', function() {
    const configDiv = document.getElementById('header-override-config');
    this.checked ? StyleUtils.show(configDiv) : StyleUtils.hide(configDiv);
});

// Add header override rule
function addHeaderOverrideRule(headerName = '', headerValue = '') {
    const rulesList = document.getElementById('header-override-rules-list');
    
    const ruleDiv = document.createElement('div');
    ruleDiv.className = 'row mb-2 header-override-rule';
    
    // 检查翻译系统是否可用
    const headerNamePlaceholder = typeof T === 'function' ? T('header_name_placeholder', 'Header名称 (如: User-Agent)') : 'Header名称 (如: User-Agent)';
    const headerValuePlaceholder = typeof T === 'function' ? T('header_value_placeholder', 'Header值 (留空删除)') : 'Header值 (留空删除)';
    
    ruleDiv.innerHTML = `
        <div class="col-4">
            <input type="text" class="form-control header-name-input" 
                   placeholder="${headerNamePlaceholder}" value="${escapeHtml(headerName)}">
        </div>
        <div class="col-6">
            <input type="text" class="form-control header-value-input" 
                   placeholder="${headerValuePlaceholder}" value="${escapeHtml(headerValue)}">
        </div>
        <div class="col-2">
            <button type="button" class="btn btn-outline-danger btn-sm" onclick="removeHeaderOverrideRule(this)">
                <i class="fas fa-trash"></i>
            </button>
        </div>
    `;
    
    rulesList.appendChild(ruleDiv);
}

// Remove header override rule
function removeHeaderOverrideRule(button) {
    button.closest('.header-override-rule').remove();
}

// Collect header override configuration data
function collectHeaderOverrideData() {
    const enabled = document.getElementById('header-override-enabled').checked;
    if (!enabled) {
        return null;
    }

    const overrides = {};
    document.querySelectorAll('.header-override-rule').forEach(ruleDiv => {
        const headerName = ruleDiv.querySelector('.header-name-input').value.trim();
        const headerValue = ruleDiv.querySelector('.header-value-input').value; // 不trim，允许空字符串
        
        if (headerName) {
            overrides[headerName] = headerValue;
        }
    });

    return Object.keys(overrides).length > 0 ? overrides : null;
}

// Load header override configuration to form
function loadHeaderOverrideConfig(overrides) {
    const checkbox = document.getElementById('header-override-enabled');
    const configDiv = document.getElementById('header-override-config');
    const rulesList = document.getElementById('header-override-rules-list');
    
    // Clear existing rules
    rulesList.innerHTML = '';
    
    if (overrides && Object.keys(overrides).length > 0) {
        checkbox.checked = true;
        StyleUtils.show(configDiv);
        
        Object.entries(overrides).forEach(([headerName, headerValue]) => {
            addHeaderOverrideRule(headerName, headerValue);
        });
    } else {
        checkbox.checked = false;
        StyleUtils.hide(configDiv);
    }
}

// ===== Request Parameter Override Functions =====


// Add parameter override rule
function addParameterOverrideRule(paramName = '', paramValue = '') {
    const rulesList = document.getElementById('parameter-override-rules-list');
    
    const ruleDiv = document.createElement('div');
    ruleDiv.className = 'row mb-2 parameter-override-rule';
    ruleDiv.innerHTML = `
        <div class="col-4">
            <input type="text" class="form-control parameter-name-input" 
                   placeholder="参数名称 (如: max_tokens)" value="${escapeHtml(paramName)}">
        </div>
        <div class="col-6">
            <input type="text" class="form-control parameter-value-input" 
                   placeholder="参数值 (留空删除)" value="${escapeHtml(paramValue)}">
        </div>
        <div class="col-2">
            <button type="button" class="btn btn-outline-danger btn-sm" onclick="removeParameterOverrideRule(this)">
                <i class="fas fa-trash"></i>
            </button>
        </div>
    `;
    
    rulesList.appendChild(ruleDiv);
}

// Remove parameter override rule
function removeParameterOverrideRule(button) {
    button.closest('.parameter-override-rule').remove();
}

// Collect parameter override configuration data
function collectParameterOverrideData() {
    const enabled = document.getElementById('parameter-override-enabled').checked;
    if (!enabled) {
        return null;
    }

    const overrides = {};
    document.querySelectorAll('.parameter-override-rule').forEach(ruleDiv => {
        const paramName = ruleDiv.querySelector('.parameter-name-input').value.trim();
        const paramValue = ruleDiv.querySelector('.parameter-value-input').value; // 不trim，允许空字符串
        
        if (paramName) {
            overrides[paramName] = paramValue;
        }
    });

    return Object.keys(overrides).length > 0 ? overrides : null;
}

// Load parameter override configuration to form
function loadParameterOverrideConfig(overrides) {
    const checkbox = document.getElementById('parameter-override-enabled');
    const configDiv = document.getElementById('parameter-override-config');
    const rulesList = document.getElementById('parameter-override-rules-list');
    
    // Clear existing rules
    rulesList.innerHTML = '';
    
    if (overrides && Object.keys(overrides).length > 0) {
        checkbox.checked = true;
        StyleUtils.show(configDiv);
        
        Object.entries(overrides).forEach(([paramName, paramValue]) => {
            addParameterOverrideRule(paramName, paramValue);
        });
    } else {
        checkbox.checked = false;
        StyleUtils.hide(configDiv);
    }
}

// Add event listener for parameter override enable/disable toggle and buttons
document.addEventListener('DOMContentLoaded', function() {
    // Parameter override enable/disable toggle
    const parameterOverrideEnabled = document.getElementById('parameter-override-enabled');
    if (parameterOverrideEnabled) {
        parameterOverrideEnabled.addEventListener('change', function() {
            const configDiv = document.getElementById('parameter-override-config');
            this.checked ? StyleUtils.show(configDiv) : StyleUtils.hide(configDiv);
        });
    }

    // Add header override rule button event listener
    const addHeaderRuleBtn = document.querySelector('[data-action="add-header-override-rule"]');
    if (addHeaderRuleBtn) {
        addHeaderRuleBtn.addEventListener('click', function() {
            addHeaderOverrideRule();
        });
    }

    // Add parameter override rule button event listener
    const addParameterRuleBtn = document.querySelector('[data-action="add-parameter-override-rule"]');
    if (addParameterRuleBtn) {
        addParameterRuleBtn.addEventListener('click', function() {
            addParameterOverrideRule();
        });
    }
});

// ===== Reorder System Messages First Functions =====

// Collect reorder system messages first configuration
function collectReorderSystemMessagesFirstData() {
    return document.getElementById('reorder-system-messages-first-enabled').checked;
}

// Load reorder system messages first configuration to form
function loadReorderSystemMessagesFirstConfig(enabled) {
    const checkbox = document.getElementById('reorder-system-messages-first-enabled');
    checkbox.checked = enabled === true;
}