import { create } from 'jsondiffpatch';
import { format as jsonpatchFormat } from 'jsondiffpatch/formatters/jsonpatch';

/**
 * Default object hashing function for array diffing.
 * Extracts `id` property if present, otherwise falls back to JSON string.
 */
function defaultHashFunction(obj) {
  if (obj && typeof obj === 'object') {
    if ('id' in obj) {
      return String(obj.id);
    }
    if ('key' in obj) {
      return String(obj.key);
    }
  }
  return JSON.stringify(obj);
}

/**
 * Computes a list of JSON Patch operations (RFC6902) to transform oldState into newState.
 *
 * @param {*} oldState
 * @param {*} newState
 * @param {{ hashFunction?: function }} options
 * @returns {Array} List of patch operations
 */
export function deriveOps(oldState, newState, options = {}) {
  const hashFunction = options.hashFunction || defaultHashFunction;
  const diffpatcher = create({ objectHash: hashFunction });
  const delta = diffpatcher.diff(oldState, newState);
  if (delta === undefined) {
    return [];
  }
  // Format to JSON Patch operations
  const ops = jsonpatchFormat(delta, oldState);
  return ops;
}