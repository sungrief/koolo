// @ts-check

import { buildField, createDragHandle, parseOptionalNumber } from "../utils.js";

/**
 * @typedef {import("../constants.js").DifficultyKey} DifficultyKey
 * @typedef {import("../SequenceEditorState.js").SequenceEditorState} SequenceEditorState
 * @typedef {import("../SequenceDataAdapter.js").SequenceDataAdapter} SequenceDataAdapter
 * @typedef {import("../SequenceDataAdapter.js").SequenceConfigEntry} SequenceConfigEntry
 * @typedef {import("../renderers/DragReorderManager.js").DragReorderManager} DragReorderManager
 * @typedef {import("../dom/DomTargetResolver.js").DomTargetResolver} DomTargetResolver
 */

/** Manages rendering for the config override section. */
export class ConfigSectionRenderer {
  constructor({
    state,
    dataAdapter,
    ensureConfigList,
    markDirty,
    dragManager,
    isConfigEditing,
    setConfigEditing,
    domTargets,
  }) {
    this.state = state;
    this.dataAdapter = dataAdapter;
    this.ensureConfigList = ensureConfigList;
    this.markDirty = markDirty;
    this.dragManager = dragManager;
    this.isConfigEditing = isConfigEditing;
    this.setConfigEditing = setConfigEditing;
    this.domTargets = domTargets;

    if (!this.domTargets) {
      throw new Error("ConfigSectionRenderer requires a domTargets resolver instance");
    }
  }

  /**
   * @param {DifficultyKey} difficulty
   */
  render(difficulty) {
    const container = this.domTargets.getConfigContainer(difficulty);
    if (!container || !this.state.data) {
      return;
    }

    const list = this.ensureConfigList(this.state.data[difficulty]);
    container.innerHTML = "";

    if (list.length === 0) {
      const empty = document.createElement("div");
      empty.className = "muted";
      empty.textContent = "No config overrides defined.";
      container.appendChild(empty);
      this.dragManager.teardown(container);
      return;
    }

    list.forEach((config) => {
      this.state.ensureEntryUID(config);
      const editing = this.isConfigEditing(difficulty, config);

      const row = document.createElement("div");
      row.className = "sequence-row config-row";
      row.classList.toggle("editing", editing);
      row.dataset.uid = String(config.__uid);

      const rowMain = document.createElement("div");
      rowMain.className = "row-main";

      const handle = createDragHandle("Reorder config adjustment");
      rowMain.appendChild(handle);

      const title = document.createElement("div");
      title.className = "row-title";
      title.textContent = "Config Override";
      const content = document.createElement("div");
      content.className = "row-content";
      content.appendChild(title);

      const summary = document.createElement("div");
      summary.className = "row-summary";

      const refreshSummary = () => {
        summary.textContent = this.buildConfigSummary(config);
        summary.classList.toggle("empty", summary.textContent === "No adjustments");
      };

      refreshSummary();
      content.appendChild(summary);
      rowMain.appendChild(content);

      const actions = document.createElement("div");
      actions.className = "row-actions";

      const editButton = document.createElement("button");
      editButton.type = "button";
      editButton.className = "btn small outline";
      editButton.textContent = editing ? "Done" : "Edit";
      editButton.addEventListener("click", () => {
        this.setConfigEditing(difficulty, config, !editing);
        this.render(difficulty);
      });
      actions.appendChild(editButton);

      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "btn small danger";
      remove.textContent = "Remove";
      remove.addEventListener("click", () => {
        if (confirm("Remove this config block?")) {
          const listRef = this.ensureConfigList(this.state.data[difficulty]);
          const currentIndex = listRef.indexOf(config);
          this.setConfigEditing(difficulty, config, false);
          if (currentIndex !== -1) {
            listRef.splice(currentIndex, 1);
          }
          this.render(difficulty);
          this.markDirty();
        }
      });
      actions.appendChild(remove);

      rowMain.appendChild(actions);
      row.appendChild(rowMain);

      if (editing) {
        row.appendChild(this.buildEditor(config, refreshSummary));
      }

      container.appendChild(row);
    });

    this.dragManager.attach(container, list, () => this.render(difficulty));
  }

  /**
   * @param {SequenceConfigEntry} config
   * @param {() => void} refreshSummary
   * @returns {HTMLDivElement}
   */
  buildEditor(config, refreshSummary) {
    const editor = document.createElement("div");
    editor.className = "config-editor";

    const levelInput = document.createElement("input");
    levelInput.type = "number";
    levelInput.placeholder = "Apply at level";
    levelInput.value = config.level != null ? String(config.level) : "";
    levelInput.addEventListener("input", (event) => {
      const target = /** @type {HTMLInputElement} */ (event.target);
      config.level = parseOptionalNumber(target.value);
      refreshSummary();
      this.markDirty();
    });
    editor.appendChild(buildField("Character level", levelInput, "config-editor-field"));

    if (!config.healthSettings) {
      config.healthSettings = {};
    }

    const grid = document.createElement("div");
    grid.className = "config-editor-grid";

    this.healthFieldDefinitions().forEach(([field, editLabel]) => {
      const input = document.createElement("input");
      input.type = "number";
      input.placeholder = "Value";
      input.value = config.healthSettings[field] != null ? String(config.healthSettings[field]) : "";
      input.addEventListener("input", (event) => {
        const target = /** @type {HTMLInputElement} */ (event.target);
        config.healthSettings[field] = parseOptionalNumber(target.value);
        refreshSummary();
        this.markDirty();
      });
      grid.appendChild(buildField(editLabel, input, "config-editor-field"));
    });

    editor.appendChild(grid);
    return editor;
  }

  /**
   * @param {SequenceConfigEntry|null} config
   * @returns {string}
   */
  buildConfigSummary(config) {
    if (!config) {
      return "No adjustments";
    }

    const parts = [];
    if (config.level != null) {
      parts.push(`Level ≥ ${config.level}`);
    }

    const settings = config.healthSettings || {};
    this.healthFieldDefinitions().forEach(([field, _editLabel, summaryLabel]) => {
      const value = settings[field];
      if (value != null) {
        parts.push(`${summaryLabel} @ ${value}%`);
      }
    });

    return parts.length ? parts.join(" • ") : "No adjustments";
  }

  /**
   * @returns {Array<[string,string,string]>}
   */
  healthFieldDefinitions() {
    return this.dataAdapter.healthFieldDefinitions();
  }
}
