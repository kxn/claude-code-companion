/* Claude Code Companion - Endpoint Wizard JavaScript */
function validateWizardNameInput() {
    const nameInput = document.getElementById('endpoint-name-wizard');
    if (nameInput) {
        const v = nameInput.value;
        if (v.includes('/') || v.includes('\\')) {
            // 检查翻译系统是否可用
            if (typeof T === 'function') {
                nameInput.setCustomValidity(T('endpoint_name_invalid_chars', '端点名称不能包含 / 或 \\'));
            } else {
                nameInput.setCustomValidity('端点名称不能包含 / 或 \\');
            }
        } else {
            nameInput.setCustomValidity('');
        }
    }
}

class EndpointWizard {
    constructor() {
        this.currentStep = 1;
        this.totalSteps = 3;
        this.selectedProfile = null;
        this.profiles = [];
        this.existingEndpoints = [];
        this.modal = null;
    }

    async init() {
        this.modal = new bootstrap.Modal(document.getElementById('endpointWizardModal'));
        await this.loadProfiles();
        await this.loadExistingEndpoints();
        this.bindEvents();
        this.renderStep1();
        const nameInput = document.getElementById('endpoint-name-wizard');
        if (nameInput) {
            nameInput.addEventListener('input', validateWizardNameInput);
        }
    }

    async loadProfiles() {
        try {
            const response = await apiRequest('/admin/api/endpoint-profiles');
            if (response.ok) {
                const data = await response.json();
                this.profiles = data.profiles || [];
            } else {
                console.error('Failed to load endpoint profiles:', response.statusText);
                const message = typeof T === 'function' ? T('load_endpoint_profiles_failed', '加载端点预设配置失败') : '加载端点预设配置失败';
                showAlert(message, 'danger');
            }
        } catch (error) {
            console.error('Error loading profiles:', error);
            const message = typeof T === 'function' ? T('load_endpoint_profiles_failed', '加载端点预设配置失败') : '加载端点预设配置失败';
            showAlert(message, 'danger');
        }
    }

    async loadExistingEndpoints() {
        try {
            const response = await apiRequest('/admin/api/endpoints');
            if (response.ok) {
                const data = await response.json();
                this.existingEndpoints = data.endpoints || [];
            } else {
                console.error('Failed to load existing endpoints:', response.statusText);
            }
        } catch (error) {
            console.error('Error loading existing endpoints:', error);
        }
    }

    bindEvents() {
        // 模态框显示事件
        document.getElementById('endpointWizardModal').addEventListener('show.bs.modal', () => {
            this.resetWizard();
        });

        // 预设选择变化事件
        document.getElementById('profile-select').addEventListener('change', (e) => {
            this.onProfileSelect(e.target.value);
        });

        // 认证信息显示/隐藏切换
        document.getElementById('toggle-auth-wizard').addEventListener('click', () => {
            this.toggleAuthVisibility();
        });

        // 导航按钮事件
        document.getElementById('wizard-prev-btn').addEventListener('click', () => {
            this.previousStep();
        });

        document.getElementById('wizard-next-btn').addEventListener('click', () => {
            this.nextStep();
        });

        document.getElementById('wizard-save-btn').addEventListener('click', () => {
            this.saveEndpoint();
        });

        // 表单验证事件
        document.getElementById('wizard-config-form').addEventListener('input', () => {
            this.validateCurrentStep();
        });
    }

    resetWizard() {
        this.currentStep = 1;
        this.selectedProfile = null;
        this.updateStepIndicator();
        this.showStep(1);
        this.updateNavigationButtons();
        this.clearAlerts();
        this.clearForm();
        
        // 重置所有按钮状态
        const saveBtn = document.getElementById('wizard-save-btn');
        const cancelBtn = document.querySelector('#endpointWizardModal [data-bs-dismiss="modal"]');
        const prevBtn = document.getElementById('wizard-prev-btn');
        const nextBtn = document.getElementById('wizard-next-btn');
        
        if (saveBtn) {
            saveBtn.disabled = false;
            const saveText = typeof T === 'function' ? T('save_endpoint', '保存端点') : '保存端点';
            saveBtn.textContent = saveBtn.getAttribute('data-t') ? 
                document.querySelector('[data-t="save_endpoint"]')?.textContent || saveText : saveText;
        }
        if (cancelBtn) cancelBtn.disabled = false;
        if (prevBtn) prevBtn.disabled = false;
        if (nextBtn) nextBtn.disabled = false;
    }

    clearForm() {
        document.getElementById('profile-select').value = '';
        document.getElementById('endpoint-name-wizard').value = '';
        document.getElementById('auth-value-wizard').value = '';
        document.getElementById('url-wizard').value = '';
        document.getElementById('default-model-input-wizard').value = '';
        document.getElementById('model-alias-wizard').value = '';

        // 隐藏默认模型组
        document.getElementById('default-model-group-wizard').classList.add('d-none');
        document.getElementById('model-alias-group-wizard').classList.add('d-none');
    }

    clearAlerts() {
        document.getElementById('wizard-error-alert').classList.add('d-none');
        document.getElementById('wizard-success-alert').classList.add('d-none');
    }

    renderStep1() {
        const select = document.getElementById('profile-select');
        const selectText = typeof T === 'function' ? T('select_endpoint_type', '请选择端点类型...') : '请选择端点类型...';
        select.innerHTML = `<option value="">${selectText}</option>`;
        
        this.profiles.forEach(profile => {
            const option = document.createElement('option');
            option.value = profile.profile_id;
            option.textContent = profile.display_name;
            select.appendChild(option);
        });
    }

    onProfileSelect(profileId) {
        if (!profileId) {
            this.selectedProfile = null;
            return;
        }

        this.selectedProfile = this.profiles.find(p => p.profile_id === profileId);
        if (this.selectedProfile) {
            console.log('Selected profile:', this.selectedProfile);
        }
    }

    renderStep2() {
        if (!this.selectedProfile) return;

        // 自动生成端点名称
        this.generateUniqueEndpointName(this.selectedProfile.profile_id);

        // 设置认证信息标签和提示
        this.setupAuthFields();

        // 自动填充URL
        document.getElementById('url-wizard').value = this.selectedProfile.url;

        // 处理默认模型字段
        this.handleDefaultModelField();
    }

    async generateUniqueEndpointName(profileId) {
        try {
            const response = await apiRequest('/admin/api/endpoints/generate-name', {
                method: 'POST',
                body: JSON.stringify({ profile_id: profileId })
            });

            if (response.ok) {
                const data = await response.json();
                document.getElementById('endpoint-name-wizard').value = data.suggested_name || profileId;
            } else {
                // 回退到本地生成
                const normalizedName = this.normalizeEndpointName(profileId);
                document.getElementById('endpoint-name-wizard').value = normalizedName;
            }
        } catch (error) {
            console.error('Error generating endpoint name:', error);
            // 回退到本地生成
            const normalizedName = this.normalizeEndpointName(profileId);
            document.getElementById('endpoint-name-wizard').value = normalizedName;
        }
    }

    normalizeEndpointName(profileId) {
        // 简单的名称标准化逻辑
        return profileId.toLowerCase()
            .replace(/[^a-z0-9\s-]/g, '')
            .replace(/\s+/g, '-')
            .replace(/-+/g, '-')
            .replace(/^-+|-+$/g, '');
    }

    setupAuthFields() {
        const authLabel = document.getElementById('auth-label-wizard');
        const authHelp = document.getElementById('auth-help-wizard');
        
        if (this.selectedProfile.auth_type === 'api_key') {
            authLabel.innerHTML = '<i class="fas fa-key form-label-icon"></i>API Key <span class="text-danger">*</span>';
            const hintText = typeof T === 'function' ? T('enter_api_key_hint', '请输入您的 API Key（如：sk-ant-api03-...）') : '请输入您的 API Key（如：sk-ant-api03-...）';
            authHelp.textContent = hintText;
        } else {
            authLabel.innerHTML = '<i class="fas fa-key form-label-icon"></i>API Token <span class="text-danger">*</span>';
            const hintText = typeof T === 'function' ? T('enter_api_token_hint', '请输入您的 API Token（如：sk-...）') : '请输入您的 API Token（如：sk-...）';
            authHelp.textContent = hintText;
        }
    }

    handleDefaultModelField() {
        const modelGroup = document.getElementById('default-model-group-wizard');
        const modelInput = document.getElementById('default-model-input-wizard');
        const datalist = document.getElementById('model-options');
        const aliasGroup = document.getElementById('model-alias-group-wizard');
        const aliasInput = document.getElementById('model-alias-wizard');

        if (!this.selectedProfile.require_default_model) {
            modelGroup.classList.add('d-none');
            aliasGroup.classList.add('d-none');
            aliasInput.value = '';
            return;
        }

        modelGroup.classList.remove('d-none');
        if (this.selectedProfile.endpoint_type === 'openai') {
            aliasGroup.classList.remove('d-none');
            aliasInput.disabled = false;
        } else {
            aliasGroup.classList.add('d-none');
            aliasInput.value = '';
        }

        // 清空datalist
        datalist.innerHTML = '';

        // 如果有预设选项，填充到datalist中
        if (this.selectedProfile.default_model_options) {
            const options = this.selectedProfile.default_model_options.split(',');
            options.forEach(option => {
                const optionElement = document.createElement('option');
                optionElement.value = option.trim();
                datalist.appendChild(optionElement);
            });
        }

        // 清空输入框
        modelInput.value = '';
        aliasInput.value = '';
    }

    renderStep3() {
        // 填充确认信息
        document.getElementById('confirm-name').textContent = document.getElementById('endpoint-name-wizard').value;
        document.getElementById('confirm-type').textContent = this.selectedProfile.display_name;
        document.getElementById('confirm-url').textContent = document.getElementById('url-wizard').value;
        
        // 设置认证方式显示
        const authTypeText = this.selectedProfile.auth_type === 'api_key' ? 'API Key' : 'API Token';
        document.getElementById('confirm-auth-type').textContent = authTypeText;

        // 处理默认模型行
        const defaultModel = document.getElementById('default-model-input-wizard').value;
        const modelRow = document.getElementById('confirm-model-row');
        if (defaultModel) {
            document.getElementById('confirm-model').textContent = defaultModel;
            modelRow.classList.remove('d-none');
        } else {
            modelRow.classList.add('d-none');
        }

        // 处理模型简称行
        const modelAlias = document.getElementById('model-alias-wizard').value;
        const aliasRow = document.getElementById('confirm-alias-row');
        if (modelAlias) {
            document.getElementById('confirm-alias').textContent = modelAlias;
            aliasRow.classList.remove('d-none');
        } else {
            aliasRow.classList.add('d-none');
        }

        // 处理路径前缀行
        const pathRow = document.getElementById('confirm-path-row');
        if (this.selectedProfile.path_prefix) {
            document.getElementById('confirm-path').textContent = this.selectedProfile.path_prefix;
            pathRow.classList.remove('d-none');
        } else {
            pathRow.classList.add('d-none');
        }

    }

    showStep(step) {
        // 隐藏所有步骤
        document.querySelectorAll('.wizard-step').forEach(stepEl => {
            stepEl.classList.remove('active');
        });

        // 显示当前步骤
        const currentStepEl = document.getElementById(`wizard-step-${step}`);
        if (currentStepEl) {
            currentStepEl.classList.add('active');
        }

        // 根据步骤执行特定逻辑
        if (step === 2) {
            this.renderStep2();
        } else if (step === 3) {
            this.renderStep3();
        }
    }

    updateStepIndicator() {
        document.querySelectorAll('.step-item').forEach((item, index) => {
            const stepNum = index + 1;
            item.classList.remove('active', 'completed');
            
            if (stepNum < this.currentStep) {
                item.classList.add('completed');
            } else if (stepNum === this.currentStep) {
                item.classList.add('active');
            }
        });
    }

    updateNavigationButtons() {
        const prevBtn = document.getElementById('wizard-prev-btn');
        const nextBtn = document.getElementById('wizard-next-btn');
        const saveBtn = document.getElementById('wizard-save-btn');

        // 上一步按钮
        prevBtn.style.display = this.currentStep > 1 ? 'inline-block' : 'none';

        // 下一步和保存按钮
        if (this.currentStep < this.totalSteps) {
            nextBtn.style.display = 'inline-block';
            saveBtn.classList.add('d-none');
        } else {
            nextBtn.style.display = 'none';
            saveBtn.classList.remove('d-none');
        }
    }

    validateCurrentStep() {
        switch (this.currentStep) {
            case 1:
                return !!this.selectedProfile;
            case 2:
                return this.validateStep2();
            case 3:
                return true; // 第三步只是确认，总是有效
            default:
                return false;
        }
    }

    validateStep2() {
        const name = document.getElementById('endpoint-name-wizard').value.trim();
        const authValue = document.getElementById('auth-value-wizard').value.trim();
        const url = document.getElementById('url-wizard').value.trim();
        const defaultModel = document.getElementById('default-model-input-wizard').value.trim();

        if (!name || !authValue || !url) {
            return false;
        }

        if (this.selectedProfile.require_default_model && !defaultModel) {
            return false;
        }

        return true;
    }

    previousStep() {
        if (this.currentStep > 1) {
            this.currentStep--;
            this.updateStepIndicator();
            this.showStep(this.currentStep);
            this.updateNavigationButtons();
            this.clearAlerts();
        }
    }

    nextStep() {
        if (!this.validateCurrentStep()) {
            const message = typeof T === 'function' ? T('complete_required_fields', '请完成当前步骤的所有必填项') : '请完成当前步骤的所有必填项';
            this.showError(message);
            return;
        }

        if (this.currentStep < this.totalSteps) {
            this.currentStep++;
            this.updateStepIndicator();
            this.showStep(this.currentStep);
            this.updateNavigationButtons();
            this.clearAlerts();
        }
    }

    async saveEndpoint() {
        if (!this.validateCurrentStep()) {
            const message = typeof T === 'function' ? T('config_validation_failed', '配置验证失败，请检查所有必填项') : '配置验证失败，请检查所有必填项';
            this.showError(message);
            return;
        }

        const saveBtn = document.getElementById('wizard-save-btn');
        const cancelBtn = document.querySelector('[data-bs-dismiss="modal"]');
        const originalSaveText = saveBtn.textContent;
        
        try {
            // 禁用所有按钮并显示加载状态
            saveBtn.disabled = true;
            cancelBtn.disabled = true;
            const savingText = typeof T === 'function' ? T('saving', '保存中...') : '保存中...';
            saveBtn.innerHTML = `<i class="fas fa-spinner fa-spin"></i> ${savingText}`;
            this.clearAlerts();

            const requestData = {
                profile_id: this.selectedProfile.profile_id,
                name: document.getElementById('endpoint-name-wizard').value.trim(),
                auth_value: document.getElementById('auth-value-wizard').value.trim(),
                url: document.getElementById('url-wizard').value.trim(),
                default_model: document.getElementById('default-model-input-wizard').value.trim(),
                model_alias: document.getElementById('model-alias-wizard').value.trim()
            };

            const response = await apiRequest('/admin/api/endpoints/from-wizard', {
                method: 'POST',
                body: JSON.stringify(requestData)
            });

            if (response.ok) {
                const data = await response.json();
                
                // 成功：关闭对话框并显示顶部成功提示
                this.modal.hide();
                
                // 使用全局的 showAlert 函数显示成功消息
                const successMessage = typeof T === 'function' ? 
                    T('endpoint_created_success', '端点 "{0}" 创建成功！').replace('{0}', data.endpoint.Name) : 
                    `端点 "${data.endpoint.Name}" 创建成功！`;
                showAlert(successMessage, 'success');
                
                // 刷新端点列表
                if (window.loadEndpointData) {
                    window.loadEndpointData();
                }
            } else {
                const errorData = await response.json();
                const errorMessage = typeof T === 'function' ? T('create_endpoint_failed', '创建端点失败') : '创建端点失败';
                this.showError(errorData.error || errorMessage);
                
                // 失败：恢复按钮状态，保持对话框打开
                saveBtn.disabled = false;
                cancelBtn.disabled = false;
                saveBtn.textContent = originalSaveText;
            }
        } catch (error) {
            console.error('Error saving endpoint:', error);
            const errorMessage = typeof T === 'function' ? T('save_endpoint_error', '保存端点时发生错误') : '保存端点时发生错误';
            this.showError(errorMessage);
            
            // 错误：恢复按钮状态，保持对话框打开
            saveBtn.disabled = false;
            cancelBtn.disabled = false;
            saveBtn.textContent = originalSaveText;
        }
    }

    toggleAuthVisibility() {
        const authInput = document.getElementById('auth-value-wizard');
        const toggleBtn = document.getElementById('toggle-auth-wizard');
        const icon = toggleBtn.querySelector('i');
        
        if (authInput.type === 'password') {
            authInput.type = 'text';
            icon.classList.remove('fa-eye');
            icon.classList.add('fa-eye-slash');
        } else {
            authInput.type = 'password';
            icon.classList.remove('fa-eye-slash');
            icon.classList.add('fa-eye');
        }
    }

    showError(message) {
        const errorAlert = document.getElementById('wizard-error-alert');
        const errorMessage = document.getElementById('wizard-error-message');
        
        errorMessage.textContent = message;
        errorAlert.classList.remove('d-none');
        
        // 隐藏成功消息
        document.getElementById('wizard-success-alert').classList.add('d-none');
    }

    showSuccess(message) {
        const successAlert = document.getElementById('wizard-success-alert');
        const successMessage = document.getElementById('wizard-success-message');
        
        successMessage.textContent = message;
        successAlert.classList.remove('d-none');
        
        // 隐藏错误消息
        document.getElementById('wizard-error-alert').classList.add('d-none');
    }

    // 公共方法：显示向导
    show() {
        this.modal.show();
    }
}

// 全局变量和初始化
let endpointWizard = null;

// 页面加载完成后初始化
document.addEventListener('DOMContentLoaded', async function() {
    // 等待翻译系统加载完成
    function waitForTranslationSystem() {
        // Check if translation system is ready
        if (typeof T !== 'function' || !window.I18n) {
            console.log('Translation system not ready for endpoint wizard, waiting...');
            setTimeout(waitForTranslationSystem, 100);
            return;
        }
        
        // Check if translations are loaded
        const allTranslations = window.I18n.getAllTranslations();
        const currentLang = window.I18n.getLanguage();
        if (!allTranslations[currentLang] || Object.keys(allTranslations[currentLang]).length === 0) {
            console.log('Translations not loaded yet for endpoint wizard, waiting...');
            setTimeout(waitForTranslationSystem, 100);
            return;
        }
        
        console.log('Translations loaded, initializing endpoint wizard...');
        
        // 初始化endpoint wizard
        if (document.getElementById('endpointWizardModal')) {
            endpointWizard = new EndpointWizard();
            endpointWizard.init().then(() => {
                // 将实例绑定到window对象以供外部调用
                window.endpointWizard = endpointWizard;
            }).catch(error => {
                console.error('Failed to initialize endpoint wizard:', error);
            });
        }
    }
    
    waitForTranslationSystem();
});