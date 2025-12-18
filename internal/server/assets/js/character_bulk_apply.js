// Bulk apply settings to multiple supervisors
document.addEventListener('DOMContentLoaded', function () {
    const modal = document.getElementById('supervisorModal');
    const openButtons = [];
    const bulkOpenButton = document.getElementById('bulkApplyOpenBtn');
    const closeButton = document.getElementById('supervisorModalCloseBtn') || (modal && modal.querySelector('.btn-close'));
    const cancelButton = document.getElementById('supervisorModalCancelBtn') || (modal && modal.querySelector('.modal-footer .btn-secondary'));
    const applyButton = document.getElementById('supervisorApplyBtn');
    const supervisorList = document.getElementById('supervisorList');
    const selectAllCheckbox = document.getElementById('selectAllChars');

    if (!modal || !supervisorList || !applyButton) {
        return;
    }

    // Track original section values for "changed" indicators
    const initialSectionState = {
        health: '',
        merc: '',
        runs: '',
        packet: '',
        cube: '',
        general: '',
        client: '',
    };
    const sectionDirty = {
        health: false,
        merc: false,
        runs: false,
        packet: false,
        cube: false,
        general: false,
        client: false,
    };

    function snapshotHealthState() {
        const form = document.querySelector('form');
        if (!form) {
            return '';
        }
        const getVal = (name) => {
            const el = form.elements.namedItem(name);
            if (!el) {
                return '';
            }
            if (el.type === 'checkbox') {
                return el.checked ? '1' : '0';
            }
            return el.value ?? '';
        };
        const state = {
            healingPotionAt: getVal('healingPotionAt'),
            manaPotionAt: getVal('manaPotionAt'),
            rejuvPotionAtLife: getVal('rejuvPotionAtLife'),
            rejuvPotionAtMana: getVal('rejuvPotionAtMana'),
            chickenAt: getVal('chickenAt'),
            townChickenAt: getVal('townChickenAt'),
        };
        return JSON.stringify(state);
    }

    function snapshotMercState() {
        const form = document.querySelector('form');
        if (!form) {
            return '';
        }
        const getVal = (name) => {
            const el = form.elements.namedItem(name);
            if (!el) {
                return '';
            }
            if (el.type === 'checkbox') {
                return el.checked ? '1' : '0';
            }
            return el.value ?? '';
        };
        const state = {
            useMerc: getVal('useMerc'),
            mercHealingPotionAt: getVal('mercHealingPotionAt'),
            mercRejuvPotionAt: getVal('mercRejuvPotionAt'),
            mercChickenAt: getVal('mercChickenAt'),
        };
        return JSON.stringify(state);
    }

    function snapshotRunsState() {
        const runsInput = document.getElementById('gameRuns');
        return runsInput ? (runsInput.value || '') : '';
    }

    function snapshotPacketState() {
        const state = {
            useForItemPickup: !!document.querySelector('input[name="packetCastingUseForItemPickup"]')?.checked,
            useForTpInteraction: !!document.querySelector('input[name="packetCastingUseForTpInteraction"]')?.checked,
            useForEntranceInteraction: !!document.querySelector('input[name="packetCastingUseForEntranceInteraction"]')?.checked,
            useForTeleport: !!document.querySelector('input[name="packetCastingUseForTeleport"]')?.checked,
            useForEntitySkills: !!document.querySelector('input[name="packetCastingUseForEntitySkills"]')?.checked,
            useForSkillSelection: !!document.querySelector('input[name="packetCastingUseForSkillSelection"]')?.checked,
        };
        return JSON.stringify(state);
    }

    function snapshotCubeState() {
        const enabled = !!document.querySelector('input[name="enableCubeRecipes"]')?.checked;
        const skipPerfectAmethysts = !!document.querySelector('input[name="skipPerfectAmethysts"]')?.checked;
        const skipPerfectRubies = !!document.querySelector('input[name="skipPerfectRubies"]')?.checked;
        const jewelsToKeepInput = document.querySelector('input[name="jewelsToKeep"]');
        const jewelsToKeep = jewelsToKeepInput ? jewelsToKeepInput.value || '' : '';
        const enabledRecipeInputs = document.querySelectorAll('input[name="enabledRecipes"]:checked');
        const enabledRecipes = Array.from(enabledRecipeInputs)
            .map(el => el.value)
            .filter(Boolean)
            .sort();
        const state = {
            enabled,
            skipPerfectAmethysts,
            skipPerfectRubies,
            jewelsToKeep,
            enabledRecipes,
        };
        return JSON.stringify(state);
    }

    function snapshotGeneralState() {
        const form = document.querySelector('form');
        if (!form) {
            return '';
        }
        const boolVal = (name) => !!form.querySelector(`input[name="${name}"]`)?.checked;
        const inputVal = (name) => {
            const el = form.elements.namedItem(name);
            return el ? (el.value || '') : '';
        };
        const state = {
            characterUseExtraBuffs: boolVal('characterUseExtraBuffs'),
            characterUseTeleport: boolVal('characterUseTeleport'),
            characterStashToShared: boolVal('characterStashToShared'),
            useCentralizedPickit: boolVal('useCentralizedPickit'),
            interactWithShrines: boolVal('interactWithShrines'),
            interactWithChests: boolVal('interactWithChests'),
            stopLevelingAt: inputVal('stopLevelingAt'),
            gameMinGoldPickupThreshold: inputVal('gameMinGoldPickupThreshold'),
            useCainIdentify: boolVal('useCainIdentify'),
            disableIdentifyTome: boolVal('game.disableIdentifyTome'),
            characterBuffOnNewArea: boolVal('characterBuffOnNewArea'),
            characterBuffAfterWP: boolVal('characterBuffAfterWP'),
            useSwapForBuffs: boolVal('useSwapForBuffs'),
            clearPathDist: inputVal('clearPathDist'),
        };
        return JSON.stringify(state);
    }

    function snapshotClientState() {
        const form = document.querySelector('form');
        if (!form) {
            return '';
        }
        const inputVal = (name) => {
            const el = form.elements.namedItem(name);
            return el ? (el.value || '') : '';
        };
        const boolVal = (name) => !!form.querySelector(`input[name="${name}"]`)?.checked;

        const state = {
            commandLineArgs: inputVal('commandLineArgs'),
            killD2OnStop: boolVal('kill_d2_process'),
            classicMode: boolVal('classic_mode'),
            closeMiniPanel: boolVal('close_mini_panel'),
            hidePortraits: boolVal('hide_portraits'),
        };
        return JSON.stringify(state);
    }

    function refreshSectionDirtyIndicators() {
        const healthCheckbox = document.getElementById('sectionHealth');
        const mercCheckbox = document.getElementById('sectionMerc');
        const runCheckbox = document.getElementById('sectionRuns');
        const packetCheckbox = document.getElementById('sectionPacketCasting');
        const cubeCheckbox = document.getElementById('sectionCubeRecipes');
        const generalCheckbox = document.getElementById('sectionGeneral');
        const clientCheckbox = document.getElementById('sectionClient');

        const healthLabelSpan = healthCheckbox && healthCheckbox.nextElementSibling;
        const mercLabelSpan = mercCheckbox && mercCheckbox.nextElementSibling;
        const runLabelSpan = runCheckbox && runCheckbox.nextElementSibling;
        const packetLabelSpan = packetCheckbox && packetCheckbox.nextElementSibling;
        const cubeLabelSpan = cubeCheckbox && cubeCheckbox.nextElementSibling;
        const generalLabelSpan = generalCheckbox && generalCheckbox.nextElementSibling;
        const clientLabelSpan = clientCheckbox && clientCheckbox.nextElementSibling;

        if (healthLabelSpan) {
            if (sectionDirty.health) {
                healthLabelSpan.classList.add('section-dirty');
            } else {
                healthLabelSpan.classList.remove('section-dirty');
            }
        }
        if (runLabelSpan) {
            if (sectionDirty.runs) {
                runLabelSpan.classList.add('section-dirty');
            } else {
                runLabelSpan.classList.remove('section-dirty');
            }
        }
        if (mercLabelSpan) {
            if (sectionDirty.merc) {
                mercLabelSpan.classList.add('section-dirty');
            } else {
                mercLabelSpan.classList.remove('section-dirty');
            }
        }
        if (packetLabelSpan) {
            if (sectionDirty.packet) {
                packetLabelSpan.classList.add('section-dirty');
            } else {
                packetLabelSpan.classList.remove('section-dirty');
            }
        }
        if (cubeLabelSpan) {
            if (sectionDirty.cube) {
                cubeLabelSpan.classList.add('section-dirty');
            } else {
                cubeLabelSpan.classList.remove('section-dirty');
            }
        }
        if (generalLabelSpan) {
            if (sectionDirty.general) {
                generalLabelSpan.classList.add('section-dirty');
            } else {
                generalLabelSpan.classList.remove('section-dirty');
            }
        }
        if (clientLabelSpan) {
            if (sectionDirty.client) {
                clientLabelSpan.classList.add('section-dirty');
            } else {
                clientLabelSpan.classList.remove('section-dirty');
            }
        }
    }

    function updateHealthDirty() {
        const current = snapshotHealthState();
        sectionDirty.health = current !== initialSectionState.health;
        refreshSectionDirtyIndicators();
    }

    function updateMercDirty() {
        const current = snapshotMercState();
        sectionDirty.merc = current !== initialSectionState.merc;
        refreshSectionDirtyIndicators();
    }

    function updateRunsDirty() {
        const current = snapshotRunsState();
        // First invocation just captures the initial state as baseline
        if (!initialSectionState.runs) {
            initialSectionState.runs = current;
            sectionDirty.runs = false;
        } else {
            sectionDirty.runs = current !== initialSectionState.runs;
        }
        refreshSectionDirtyIndicators();
    }

    function updatePacketDirty() {
        const current = snapshotPacketState();
        sectionDirty.packet = current !== initialSectionState.packet;
        refreshSectionDirtyIndicators();
    }

    function updateCubeDirty() {
        const current = snapshotCubeState();
        sectionDirty.cube = current !== initialSectionState.cube;
        refreshSectionDirtyIndicators();
    }

    function updateGeneralDirty() {
        const current = snapshotGeneralState();
        sectionDirty.general = current !== initialSectionState.general;
        refreshSectionDirtyIndicators();
    }

    function updateClientDirty() {
        const current = snapshotClientState();
        sectionDirty.client = current !== initialSectionState.client;
        refreshSectionDirtyIndicators();
    }

    // Initialize snapshots
    initialSectionState.health = snapshotHealthState();
    initialSectionState.merc = snapshotMercState();
    initialSectionState.packet = snapshotPacketState();
    initialSectionState.cube = snapshotCubeState();
    initialSectionState.general = snapshotGeneralState();
    initialSectionState.client = snapshotClientState();
    refreshSectionDirtyIndicators();

    if (bulkOpenButton) {
        openButtons.push(bulkOpenButton);
    }

    function openModal() {
        modal.style.display = 'flex';
    }

    function closeModal() {
        modal.style.display = 'none';
    }

    openButtons.forEach(btn => {
        btn.addEventListener('click', openModal);
    });

    if (closeButton) {
        closeButton.addEventListener('click', closeModal);
    }
    if (cancelButton) {
        cancelButton.addEventListener('click', closeModal);
    }

    // Normalize button labels to English for global use
    if (cancelButton) {
        cancelButton.textContent = 'Cancel';
    }
    if (applyButton) {
        applyButton.textContent = 'Apply';
    }

    // Initialize section selection UI inside the modal
    const sectionContainer = modal.querySelector('.modal-body .mb-2');
    if (sectionContainer) {
        sectionContainer.innerHTML = ''
            + '<strong>Select settings to apply:</strong>'
            + '<div class="supervisor-section-toggles">'
            + '  <label>'
            + '    <input type="checkbox" id="sectionHealth">'
            + '    <span>Health settings</span>'
            + '  </label>'
            + '  <label>'
            + '    <input type="checkbox" id="sectionMerc">'
            + '    <span>Merc settings</span>'
            + '  </label>'
            + '  <label>'
            + '    <input type="checkbox" id="sectionRuns">'
            + '    <span>Run settings</span>'
            + '  </label>'
            + '  <label>'
            + '    <input type="checkbox" id="sectionPacketCasting">'
            + '    <span>Using Packets</span>'
            + '  </label>'
            + '  <label>'
            + '    <input type="checkbox" id="sectionCubeRecipes">'
            + '    <span>Cube recipes</span>'
            + '  </label>'
            + '  <label>'
            + '    <input type="checkbox" id="sectionGeneral">'
            + '    <span>General settings</span>'
            + '  </label>'
            + '  <label>'
            + '    <input type="checkbox" id="sectionClient">'
            + '    <span>Client settings</span>'
            + '  </label>'
            + '</div>';
    }

    // Fix label text for "select all" if it contains garbled characters
    const selectAllLabel = selectAllCheckbox
        ? selectAllCheckbox.closest('.form-check')?.querySelector('.form-check-label')
        : null;
    if (selectAllLabel) {
        selectAllLabel.textContent = 'Select all supervisors';
    }

    // Load supervisor list from initial-data endpoint
    async function populateSupervisorList() {
        try {
            const response = await fetch('/initial-data', {
                headers: { 'Accept': 'application/json' },
            });
            if (!response.ok) {
                return;
            }
            const data = await response.json();
            const status = data && data.Status ? data.Status : {};
            const names = Object.keys(status);
            names.sort();

            supervisorList.innerHTML = '';

            const currentSupervisorInput = document.querySelector('input[name="name"]');
            const currentSupervisorName = currentSupervisorInput ? currentSupervisorInput.value : '';

            names.forEach(name => {
                if (!name) {
                    return;
                }
                const wrapper = document.createElement('div');
                wrapper.className = 'form-check';

                const input = document.createElement('input');
                input.type = 'checkbox';
                input.className = 'form-check-input supervisor-checkbox';
                input.id = `sup-${name}`;
                input.value = name;

                const label = document.createElement('label');
                label.className = 'form-check-label';
                label.htmlFor = input.id;
                label.textContent = name;

                if (currentSupervisorName && name === currentSupervisorName) {
                    label.classList.add('current-supervisor');
                    label.title = 'Current supervisor (this page)';
                }

                wrapper.appendChild(input);
                wrapper.appendChild(label);
                supervisorList.appendChild(wrapper);
            });
        } catch (error) {
            console.error('Failed to populate supervisor list', error);
        }
    }

    void populateSupervisorList();

    // Track changes in sections to mark as "dirty"
    const HEALTH_FIELD_NAMES = new Set([
        'healingPotionAt',
        'manaPotionAt',
        'rejuvPotionAtLife',
        'rejuvPotionAtMana',
        'chickenAt',
        'townChickenAt',
    ]);

    const MERC_FIELD_NAMES = new Set([
        'useMerc',
        'mercHealingPotionAt',
        'mercRejuvPotionAt',
        'mercChickenAt',
    ]);

    const CUBE_FIELD_NAMES = new Set([
        'enableCubeRecipes',
        'skipPerfectAmethysts',
        'skipPerfectRubies',
        'jewelsToKeep',
        'enabledRecipes',
    ]);

    const GENERAL_FIELD_NAMES = new Set([
        'characterUseExtraBuffs',
        'characterUseTeleport',
        'characterStashToShared',
        'useCentralizedPickit',
        'interactWithShrines',
        'interactWithChests',
        'stopLevelingAt',
        'gameMinGoldPickupThreshold',
        'useCainIdentify',
        'game.disableIdentifyTome',
        'characterBuffOnNewArea',
        'characterBuffAfterWP',
        'useSwapForBuffs',
        'clearPathDist',
    ]);

    const CLIENT_FIELD_NAMES = new Set([
        'commandLineArgs',
        'kill_d2_process',
        'classic_mode',
        'close_mini_panel',
        'hide_portraits',
    ]);

    document.addEventListener('change', function (event) {
        const target = event.target;
        if (!(target instanceof HTMLInputElement)) {
            return;
        }

        if (HEALTH_FIELD_NAMES.has(target.name)) {
            updateHealthDirty();
            return;
        }

        if (MERC_FIELD_NAMES.has(target.name)) {
            updateMercDirty();
            return;
        }

        if (target.name && target.name.startsWith('packetCastingUseFor')) {
            updatePacketDirty();
            return;
        }

        if (CUBE_FIELD_NAMES.has(target.name)) {
            updateCubeDirty();
            return;
        }

        if (GENERAL_FIELD_NAMES.has(target.name)) {
            updateGeneralDirty();
            return;
        }

        if (CLIENT_FIELD_NAMES.has(target.name)) {
            updateClientDirty();
        }
    });

    // Called from updateEnabledRunsHiddenField whenever the run list changes
    window.onGameRunsUpdated = updateRunsDirty;

    if (selectAllCheckbox) {
        selectAllCheckbox.addEventListener('change', function () {
            const checkboxes = supervisorList.querySelectorAll('.supervisor-checkbox');
            checkboxes.forEach(cb => {
                cb.checked = selectAllCheckbox.checked;
            });
        });
    }

    function collectFormAsJson() {
        const form = document.querySelector('form');
        const fd = new FormData(form);
        const result = {};

        fd.forEach((value, key) => {
            const asString = String(value);
            if (!Object.prototype.hasOwnProperty.call(result, key)) {
                result[key] = [asString];
            } else {
                result[key].push(asString);
            }
        });

        return result;
    }

    function getSectionsSelection() {
        const healthCheckbox = document.getElementById('sectionHealth');
        const mercCheckbox = document.getElementById('sectionMerc');
        const runsCheckbox = document.getElementById('sectionRuns');
        const packetCheckbox = document.getElementById('sectionPacketCasting');
        const cubeCheckbox = document.getElementById('sectionCubeRecipes');
        const generalCheckbox = document.getElementById('sectionGeneral');
        const clientCheckbox = document.getElementById('sectionClient');
        return {
            health: !!(healthCheckbox && healthCheckbox.checked),
            merc: !!(mercCheckbox && mercCheckbox.checked),
            runs: !!(runsCheckbox && runsCheckbox.checked),
            packetCasting: !!(packetCheckbox && packetCheckbox.checked),
            cubeRecipes: !!(cubeCheckbox && cubeCheckbox.checked),
            general: !!(generalCheckbox && generalCheckbox.checked),
            client: !!(clientCheckbox && clientCheckbox.checked),
        };
    }

    applyButton.addEventListener('click', async function () {
        const supervisorNameInput = document.querySelector('input[name="name"]');
        const currentSupervisor = supervisorNameInput ? supervisorNameInput.value : '';
        if (!currentSupervisor) {
            alert('Supervisor name is empty.');
            return;
        }

        const selectedSupervisorElems = supervisorList.querySelectorAll('.supervisor-checkbox:checked');
        const targetSupervisors = Array.from(selectedSupervisorElems)
            .map(cb => cb.value)
            .filter(name => !!name && name !== currentSupervisor);

        const sections = getSectionsSelection();
        const anySelected = Object.values(sections).some(Boolean);
        if (!anySelected) {
            alert('Please select at least one section to apply.');
            return;
        }

        const payload = {
            sourceSupervisor: currentSupervisor,
            targetSupervisors: targetSupervisors,
            sections: sections,
            form: collectFormAsJson(),
        };

        try {
            const response = await fetch('/api/supervisors/bulk-apply', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Accept': 'application/json',
                },
                body: JSON.stringify(payload),
            });

            if (!response.ok) {
                const message = await response.text();
                throw new Error(message || `Bulk apply failed (${response.status})`);
            }

            const data = await response.json().catch(() => ({}));
            if (data && data.success === false) {
                throw new Error(data.error || 'Bulk apply failed');
            }

            alert('Settings have been applied to the selected supervisors.');
            closeModal();
        } catch (error) {
            console.error('Failed to bulk apply settings', error);
            alert('An error occurred while applying settings. Please check the logs.');
        }
    });
});
