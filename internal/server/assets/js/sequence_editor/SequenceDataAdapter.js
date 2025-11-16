// @ts-check

import { DIFFICULTIES } from "./constants.js";
import { normalizeRunNumericValue, parseOptionalNumber } from "./utils.js";

/** @typedef {import("./constants.js").DifficultyKey} DifficultyKey */

/** @typedef {string|number|null|undefined} NumericLike */

/**
 * @typedef {{
 * run?: string,
 * minLevel?: NumericLike,
 * maxLevel?: NumericLike,
 * MinLevel?: NumericLike,
 * MaxLevel?: NumericLike,
 * lowGoldRun?: boolean,
 * LowGoldRun?: boolean,
 * skipTownChores?: boolean,
 * SkipTownChores?: boolean,
 * exitGame?: boolean,
 * ExitGame?: boolean,
 * stopIfCheckFails?: boolean,
 * StopIfCheckFails?: boolean,
 * parameters?: never
 * }} RawRunEntry
 */

/**
 * @typedef {{
 * level?: NumericLike,
 * Level?: NumericLike,
 * healthSettings?: Record<string, NumericLike>
 * }} RawConfigEntry
 */

/**
 * @typedef {{
 * level?: NumericLike,
 * fireRes?: NumericLike,
 * coldRes?: NumericLike,
 * lightRes?: NumericLike,
 * poisonRes?: NumericLike,
 * aboveLowGold?: boolean,
 * aboveGoldThreshold?: boolean
 * }} RawConditionEntry
 */

/**
 * @typedef {RawRunEntry & {
 * run: string,
 * minLevel?: number,
 * maxLevel?: number,
 * lowGoldRun: boolean,
 * skipTownChores: boolean,
 * exitGame: boolean,
 * stopIfCheckFails: boolean
 * }} SequenceRunEntry
 */

/**
 * @typedef {RawConfigEntry & {
 * level?: number,
 * healthSettings: Record<string, number|undefined>
 * }} SequenceConfigEntry
 */

/**
 * @typedef {RawConditionEntry & {
 * level?: number,
 * fireRes?: number,
 * coldRes?: number,
 * lightRes?: number,
 * poisonRes?: number,
 * aboveLowGold: boolean,
 * aboveGoldThreshold: boolean
 * }} SequenceConditionEntry
 */

/**
 * @typedef {{
 * beforeQuests: SequenceRunEntry[],
 * quests: SequenceRunEntry[],
 * afterQuests: SequenceRunEntry[],
 * configSettings: SequenceConfigEntry[],
 * nextDifficultyConditions?: SequenceConditionEntry,
 * stayDifficultyConditions?: SequenceConditionEntry
 * }} DifficultySettings
 */

/**
 * @typedef {{
 * beforeQuests: RawRunEntry[],
 * quests: RawRunEntry[],
 * afterQuests: RawRunEntry[],
 * configSettings: RawConfigEntry[],
 * nextDifficultyConditions?: RawConditionEntry,
 * stayDifficultyConditions?: RawConditionEntry
 * }} SerializedDifficultySettings
 */

/** @typedef {Record<DifficultyKey, SerializedDifficultySettings>} SerializedSequencePayload */

/**
 * @typedef {Partial<Record<DifficultyKey, SerializedDifficultySettings|DifficultySettings>>|null|undefined} SequenceSettingsInput
 */

/** @typedef {RawRunEntry} RunEntryInput */
/** @typedef {RawConfigEntry} ConfigEntryInput */
/** @typedef {RawConditionEntry} ConditionEntryInput */

/**
 * Performs hydration, normalization, and serialization of sequence editor data.
 */
export class SequenceDataAdapter {
  /**
   * @param {import("./SequenceEditorState.js").SequenceEditorState} state
   */
  constructor(state) {
    this.state = state;
  }

  /**
   * Loads raw server data into structured state.
   * @param {SequenceSettingsInput} raw
   * @returns {Record<DifficultyKey, DifficultySettings>}
   */
  hydrateSequenceData(raw) {
    const source = raw || {};
    const data = /** @type {Record<DifficultyKey, DifficultySettings>} */ ({});

    DIFFICULTIES.forEach((difficulty) => {
      const difficultyInput = /** @type {DifficultySettings|SerializedDifficultySettings|undefined} */ (
        source[difficulty]
      );
      data[difficulty] = this.hydrateDifficultySettings(difficultyInput);
    });

    this.state.data = data;
    this.normalizeClientData();
    return this.state.data;
  }

  /**
   * @param {DifficultySettings|SerializedDifficultySettings|undefined} raw
   * @returns {DifficultySettings}
   */
  hydrateDifficultySettings(raw) {
    const settings = this.createEmptyDifficultySettings();
    const source = /** @type {DifficultySettings|SerializedDifficultySettings} */ (raw || {});

    settings.beforeQuests = this.hydrateRunList(source.beforeQuests);
    settings.quests = this.hydrateRunList(source.quests);
    settings.afterQuests = this.hydrateRunList(source.afterQuests);
    settings.configSettings = this.hydrateConfigList(source.configSettings);
    settings.nextDifficultyConditions = this.hydrateConditionEntry(source.nextDifficultyConditions);
    settings.stayDifficultyConditions = this.hydrateConditionEntry(source.stayDifficultyConditions);

    return settings;
  }

  /**
   * @param {Array<RunEntryInput>|undefined|null} rawList
   * @returns {SequenceRunEntry[]}
   */
  hydrateRunList(rawList) {
    if (!Array.isArray(rawList)) {
      return [];
    }
    const hydrated = rawList.map((entry) => this.hydrateRunEntry(entry)).filter((entry) => Boolean(entry?.run));
    return /** @type {SequenceRunEntry[]} */ (hydrated);
  }

  /**
   * @param {RunEntryInput|null|undefined} raw
   * @returns {SequenceRunEntry|undefined}
   */
  hydrateRunEntry(raw) {
    if (!raw) {
      return undefined;
    }

    const entry = /** @type {SequenceRunEntry} */ ({
      run: typeof raw.run === "string" ? raw.run : "",
      minLevel: normalizeRunNumericValue(parseOptionalNumber(raw.minLevel ?? raw.MinLevel)),
      maxLevel: normalizeRunNumericValue(parseOptionalNumber(raw.maxLevel ?? raw.MaxLevel)),
      lowGoldRun: Boolean(raw.lowGoldRun ?? raw.LowGoldRun),
      skipTownChores: Boolean(raw.skipTownChores ?? raw.SkipTownChores),
      exitGame: Boolean(raw.exitGame ?? raw.ExitGame),
      stopIfCheckFails: Boolean(raw.stopIfCheckFails ?? raw.StopIfCheckFails),
    });

    this.state.ensureEntryUID(entry);
    return entry;
  }

  /**
   * @param {Array<ConfigEntryInput>|undefined|null} rawList
   * @returns {SequenceConfigEntry[]}
   */
  hydrateConfigList(rawList) {
    if (!Array.isArray(rawList)) {
      return [];
    }
    const hydrated = rawList.map((entry) => this.hydrateConfigEntry(entry)).filter((entry) => Boolean(entry));
    return /** @type {SequenceConfigEntry[]} */ (hydrated);
  }

  /**
   * @param {ConfigEntryInput|null|undefined} raw
   * @returns {SequenceConfigEntry|undefined}
   */
  hydrateConfigEntry(raw) {
    if (!raw) {
      return undefined;
    }

    const entry = /** @type {SequenceConfigEntry} */ ({
      level: parseOptionalNumber(raw.level ?? raw.Level),
      healthSettings: {},
    });

    const healthSource =
      raw.healthSettings && typeof raw.healthSettings === "object"
        ? /** @type {Record<string, NumericLike>} */ (raw.healthSettings)
        : {};

    this.healthFieldDefinitions().forEach(([field]) => {
      const value = parseOptionalNumber(healthSource[field]);
      if (value != null) {
        entry.healthSettings[field] = value;
      }
    });

    this.state.ensureEntryUID(entry);
    return entry;
  }

  /**
   * @param {ConditionEntryInput|null|undefined} raw
   * @returns {SequenceConditionEntry|undefined}
   */
  hydrateConditionEntry(raw) {
    if (!raw) {
      return undefined;
    }
    return this.normalizeCondition(raw);
  }

  /**
   * Serializes the editor state back into a payload consumable by the backend.
   * @returns {SerializedSequencePayload}
   */
  buildSavePayload() {
    const source = this.state.data || this.createEmptySequenceData();
    const payload = /** @type {SerializedSequencePayload} */ ({});

    DIFFICULTIES.forEach((difficulty) => {
      payload[difficulty] = this.serializeDifficultySettings(source[difficulty]);
    });

    return payload;
  }

  /**
   * @param {DifficultySettings} [settings]
   * @returns {SerializedDifficultySettings}
   */
  serializeDifficultySettings(settings) {
    const result = /** @type {SerializedDifficultySettings} */ ({
      quests: this.serializeRunSection(settings?.quests),
      beforeQuests: this.serializeRunSection(settings?.beforeQuests),
      afterQuests: this.serializeRunSection(settings?.afterQuests),
      configSettings: this.serializeConfigSection(settings?.configSettings),
    });

    if (settings?.nextDifficultyConditions) {
      const serializedNext = this.serializeConditionEntry(settings.nextDifficultyConditions);
      if (serializedNext) {
        result.nextDifficultyConditions = serializedNext;
      }
    }

    if (settings?.stayDifficultyConditions) {
      const serializedStay = this.serializeConditionEntry(settings.stayDifficultyConditions);
      if (serializedStay) {
        result.stayDifficultyConditions = serializedStay;
      }
    }

    return result;
  }

  /**
   * @param {SequenceRunEntry} entry
   * @returns {RawRunEntry|null}
   */
  serializeRunEntry(entry) {
    if (!entry || !entry.run) {
      return null;
    }

    const result = /** @type {RawRunEntry} */ ({
      run: entry.run,
    });

    if (entry.minLevel != null) {
      result.minLevel = entry.minLevel;
    }
    if (entry.maxLevel != null) {
      result.maxLevel = entry.maxLevel;
    }
    if (entry.lowGoldRun) {
      result.lowGoldRun = true;
    }
    if (entry.skipTownChores) {
      result.skipTownChores = true;
    }
    if (entry.exitGame) {
      result.exitGame = true;
    }
    if (entry.stopIfCheckFails) {
      result.stopIfCheckFails = true;
    }

    return result;
  }

  /**
   * @param {SequenceConfigEntry|null|undefined} entry
   * @returns {RawConfigEntry|null}
   */
  serializeConfigEntry(entry) {
    if (!entry) {
      return null;
    }

    const result = /** @type {RawConfigEntry} */ ({});
    if (entry.level != null) {
      result.level = entry.level;
    }

    const health = /** @type {Record<string, number>} */ ({});
    if (entry.healthSettings && typeof entry.healthSettings === "object") {
      this.healthFieldDefinitions().forEach(([field]) => {
        const value = entry.healthSettings[field];
        if (value != null) {
          health[field] = value;
        }
      });
    }

    if (Object.keys(health).length) {
      result.healthSettings = health;
    }

    if (!Object.keys(result).length) {
      return null;
    }

    return result;
  }

  /**
   * @param {SequenceConditionEntry|null|undefined} condition
   * @returns {RawConditionEntry|null}
   */
  serializeConditionEntry(condition) {
    if (!condition) {
      return null;
    }

    const result = /** @type {RawConditionEntry} */ ({
      aboveLowGold: Boolean(condition.aboveLowGold),
      aboveGoldThreshold: Boolean(condition.aboveGoldThreshold),
    });

    if (condition.level != null) {
      result.level = condition.level;
    }
    if (condition.fireRes != null) {
      result.fireRes = condition.fireRes;
    }
    if (condition.coldRes != null) {
      result.coldRes = condition.coldRes;
    }
    if (condition.lightRes != null) {
      result.lightRes = condition.lightRes;
    }
    if (condition.poisonRes != null) {
      result.poisonRes = condition.poisonRes;
    }

    return result;
  }

  /**
   * @param {SequenceRunEntry[]|null|undefined} list
   * @returns {RawRunEntry[]}
   */
  serializeRunSection(list) {
    const serialized = this.coerceList(list)
      .map((entry) => this.serializeRunEntry(entry))
      .filter(Boolean);
    return /** @type {RawRunEntry[]} */ (serialized);
  }

  /**
   * @param {SequenceConfigEntry[]|null|undefined} list
   * @returns {RawConfigEntry[]}
   */
  serializeConfigSection(list) {
    const serialized = this.coerceList(list)
      .map((entry) => this.serializeConfigEntry(entry))
      .filter(Boolean);
    return /** @type {RawConfigEntry[]} */ (serialized);
  }

  /** Normalizes editor state by ensuring predictable arrays and camelCase fields. */
  normalizeClientData() {
    if (!this.state.data) {
      return;
    }

    DIFFICULTIES.forEach((difficulty) => {
      const settings = this.state.data[difficulty];
      if (!settings) {
        this.state.data[difficulty] = this.createEmptyDifficultySettings();
        return;
      }

      settings.beforeQuests = this.coerceList(settings.beforeQuests);
      settings.quests = this.coerceList(settings.quests);
      settings.afterQuests = this.coerceList(settings.afterQuests);
      settings.configSettings = this.coerceList(settings.configSettings);

      settings.beforeQuests.forEach((entry) => this.normalizeRunEntry(entry));
      settings.quests.forEach((entry) => this.normalizeRunEntry(entry));
      settings.afterQuests.forEach((entry) => this.normalizeRunEntry(entry));
      settings.configSettings.forEach((entry) => this.normalizeConfigEntry(entry));

      if (settings.nextDifficultyConditions && typeof settings.nextDifficultyConditions === "object") {
        settings.nextDifficultyConditions = this.normalizeCondition(settings.nextDifficultyConditions);
      } else {
        settings.nextDifficultyConditions = undefined;
      }

      if (settings.stayDifficultyConditions && typeof settings.stayDifficultyConditions === "object") {
        settings.stayDifficultyConditions = this.normalizeCondition(settings.stayDifficultyConditions);
      } else {
        settings.stayDifficultyConditions = undefined;
      }
    });
  }

  /**
   * @param {SequenceRunEntry} entry
   */
  normalizeRunEntry(entry) {
    entry.run = entry.run || "";
    entry.minLevel = normalizeRunNumericValue(parseOptionalNumber(entry.minLevel ?? entry.MinLevel));
    entry.maxLevel = normalizeRunNumericValue(parseOptionalNumber(entry.maxLevel ?? entry.MaxLevel));
    delete entry.MinLevel;
    delete entry.MaxLevel;
    if (entry.StopIfCheckFails != null) {
      entry.stopIfCheckFails = Boolean(entry.StopIfCheckFails);
      delete entry.StopIfCheckFails;
    }
    entry.lowGoldRun = Boolean(entry.lowGoldRun);
    entry.skipTownChores = Boolean(entry.skipTownChores);
    entry.exitGame = Boolean(entry.exitGame);
    entry.stopIfCheckFails = Boolean(entry.stopIfCheckFails);
    if ("parameters" in entry) {
      delete entry.parameters;
    }
    this.state.ensureEntryUID(entry);
  }

  /**
   * @param {SequenceConfigEntry|undefined} entry
   */
  normalizeConfigEntry(entry) {
    if (!entry) {
      return;
    }
    entry.level = parseOptionalNumber(entry.level);
    if (!entry.healthSettings) {
      entry.healthSettings = {};
    }
    Object.keys(entry.healthSettings).forEach((key) => {
      entry.healthSettings[key] = parseOptionalNumber(entry.healthSettings[key]);
    });
    this.state.ensureEntryUID(entry);
  }

  /**
   * @param {ConditionEntryInput} condition
   * @returns {SequenceConditionEntry}
   */
  normalizeCondition(condition) {
    return {
      level: parseOptionalNumber(condition.level),
      fireRes: parseOptionalNumber(condition.fireRes),
      coldRes: parseOptionalNumber(condition.coldRes),
      lightRes: parseOptionalNumber(condition.lightRes),
      poisonRes: parseOptionalNumber(condition.poisonRes),
      aboveLowGold: Boolean(condition.aboveLowGold),
      aboveGoldThreshold: Boolean(condition.aboveGoldThreshold),
    };
  }

  /**
   * @template T
   * @param {T[]|null|undefined} value
   * @returns {T[]}
   */
  coerceList(value) {
    return Array.isArray(value) ? value : [];
  }

  /**
   * @returns {Record<DifficultyKey, DifficultySettings>}
   */
  createEmptySequenceData() {
    const data = /** @type {Record<DifficultyKey, DifficultySettings>} */ ({});

    DIFFICULTIES.forEach((difficulty) => {
      data[difficulty] = this.createEmptyDifficultySettings();
    });

    if (data.normal) {
      data.normal.nextDifficultyConditions = this.createEmptyConditions();
    }

    if (data.nightmare) {
      data.nightmare.nextDifficultyConditions = this.createEmptyConditions();
      data.nightmare.stayDifficultyConditions = undefined;
    }

    if (data.hell) {
      data.hell.stayDifficultyConditions = this.createEmptyConditions();
    }

    return data;
  }

  /**
   * @returns {DifficultySettings}
   */
  createEmptyDifficultySettings() {
    return {
      beforeQuests: [],
      quests: [],
      afterQuests: [],
      nextDifficultyConditions: undefined,
      stayDifficultyConditions: undefined,
      configSettings: [],
    };
  }

  /**
   * @returns {SequenceConditionEntry}
   */
  createEmptyConditions() {
    return /** @type {SequenceConditionEntry} */ ({
      level: undefined,
      fireRes: undefined,
      coldRes: undefined,
      lightRes: undefined,
      poisonRes: undefined,
      aboveLowGold: false,
      aboveGoldThreshold: false,
    });
  }

  /**
   * @returns {SequenceRunEntry}
   */
  createEmptyRunEntry() {
    const entry = /** @type {SequenceRunEntry} */ ({
      run: "",
      minLevel: undefined,
      maxLevel: undefined,
      lowGoldRun: false,
      skipTownChores: false,
      exitGame: false,
      stopIfCheckFails: false,
    });
    this.state.ensureEntryUID(entry);
    return entry;
  }

  /**
   * @returns {Array<[string,string,string]>}
   */
  healthFieldDefinitions() {
    return [
      ["healingPotionAt", "Healing Potion At", "Heal"],
      ["manaPotionAt", "Mana Potion At", "Mana"],
      ["rejuvPotionAtLife", "Rejuv Potion At Life", "Rejuv HP"],
      ["rejuvPotionAtMana", "Rejuv Potion At Mana", "Rejuv Mana"],
      ["mercHealingPotionAt", "Merc Healing Potion At", "Merc Heal"],
      ["mercRejuvPotionAt", "Merc Rejuv Potion At", "Merc Rejuv"],
      ["chickenAt", "Chicken At", "Chicken"],
      ["townChickenAt", "Town Chicken At", "Town"],
      ["mercChickenAt", "Merc Chicken At", "Merc Chicken"],
    ];
  }
}
