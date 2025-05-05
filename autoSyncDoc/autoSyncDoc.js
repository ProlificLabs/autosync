import * as Y from 'yjs';
import { deriveOps } from './deriveOps.js';

/**
 * A Yjs-based CRDT document for JSON structures.
 */
export class AutoSyncDoc {
  constructor() {
    this.yDoc = new Y.Doc();
    this.yDoc.getMap('root');
  }

  /**
   * Serialize the document to JSON.
   */
  toJSON() {
    return this.yDoc.getMap('root').toJSON();
  }

  /**
   * Recursively create nested Y.Map/Y.Array structures from plain JS objects/arrays.
   * @param {*} value The value to convert.
   * @returns The converted Yjs type or the original primitive value.
   */
  createNestedYType(value) {
    if (Array.isArray(value)) {
      const newArr = new Y.Array();
      // Insert recursively converted items
      newArr.insert(0, value.map(item => this.createNestedYType(item)));
      return newArr;
    } else if (value !== null && typeof value === 'object' && !(value instanceof Y.Map) && !(value instanceof Y.Array)) {
      // Handle plain objects (excluding null and existing Yjs types)
      const newMap = new Y.Map();
      for (const [key, val] of Object.entries(value)) {
        newMap.set(key, this.createNestedYType(val));
      }
      return newMap;
    } else {
      // Return primitives and Yjs types as is
      return value;
    }
  }

  /**
   * Apply RFC6902 JSON Patch operations to the CRDT document.
   * Supports add, remove, replace, move, copy on Maps and Arrays.
   * @param {Array} ops JSON Patch operations
   */
  applyOperations(ops) {
    this.yDoc.transact(() => {
      const rootMap = this.yDoc.getMap('root'); // Always start from the root map

      for (const op of ops) {
        const { op: type, path } = op;
        // Split path and drop leading empty. Path is relative to the root map.
        const targetPathSegments = path.split('/').filter(s => s);

        const parentPathSegments = targetPathSegments.slice(0, -1);
        const lastSegment = targetPathSegments[targetPathSegments.length - 1];

        // Navigate to parent container starting at root map
        let parent = rootMap;
        for (const segment of parentPathSegments) {
          // Attempt to get the next nested container
          const nextParent = parent instanceof Y.Array
            ? parent.get(Number(segment))
            : parent.get(segment);

          // Check if the next parent exists and is a valid Yjs Map or Array
          if (!(nextParent instanceof Y.Map || nextParent instanceof Y.Array)) {
             throw new Error(`Invalid or non-existent container at segment '${segment}' in path '${path}'`);
          }
          parent = nextParent;
        }

        // Determine the key/index for the operation
        const key = parent instanceof Y.Array ? Number(lastSegment) : lastSegment;

        switch (type) {
          case 'add': {
            const val = this.createNestedYType(op.value);
            if (parent instanceof Y.Array) {
              // Ensure index is within bounds for insertion
              if (key > parent.length || key < 0) {
                throw new Error(`Index ${key} out of bounds for array at path '${parentPathSegments.join('/')}'`);
              }
              parent.insert(key, [val]);
            } else if (parent instanceof Y.Map) {
              parent.set(key, val);
            } else {
              // This case should not be reached due to navigation checks
              throw new Error(`Invalid parent type ${typeof parent} at path '${parentPathSegments.join('/')}'`);
            }
            break;
          }
          case 'remove': {
            if (parent instanceof Y.Array) {
               // Ensure index is within bounds for deletion
              if (key >= parent.length || key < 0) {
                throw new Error(`Index ${key} out of bounds for array at path '${parentPathSegments.join('/')}'`);
              }
              parent.delete(key, 1);
            } else if (parent instanceof Y.Map) {
              if (!parent.has(key)) {
                 throw new Error(`Key '${key}' not found in map at path '${parentPathSegments.join('/')}'`);
              }
              parent.delete(key);
            } else {
              // This case should not be reached due to navigation checks
              throw new Error(`Invalid parent type ${typeof parent} at path '${parentPathSegments.join('/')}'`);
            }
            break;
          }
          case 'replace': {
            const val = this.createNestedYType(op.value);
            if (parent instanceof Y.Array) {
               // Ensure index is within bounds for replacement
              if (key >= parent.length || key < 0) {
                throw new Error(`Index ${key} out of bounds for array at path '${parentPathSegments.join('/')}'`);
              }
              // Yjs arrays don't have a direct replace, so delete then insert
              parent.delete(key, 1);
              parent.insert(key, [val]);
            } else if (parent instanceof Y.Map) {
               if (!parent.has(key)) {
                 throw new Error(`Key '${key}' not found for replacement in map at path '${parentPathSegments.join('/')}'`);
              }
              parent.set(key, val);
            } else {
               throw new Error(`Invalid parent type ${typeof parent} at path '${parentPathSegments.join('/')}'`);
            }
            break;
          }
          case 'move': {
            const fromPath = op.from;
            // Check if the target path is inside the source path
            if (path !== fromPath && path.startsWith(fromPath + '/')) {
              throw new Error(`Cannot move item at '${fromPath}' to a location within itself ('${path}')`);
            }

            // Source navigation: path relative to root map
            const sourcePathSegments = fromPath.split('/').filter(s => s);
            const srcParentPathSegments = sourcePathSegments.slice(0, -1);
            const fromKeySegment = sourcePathSegments[sourcePathSegments.length - 1];

            let srcParent = rootMap;
            for (const seg of srcParentPathSegments) {
               const nextSrcParent = srcParent instanceof Y.Array
                 ? srcParent.get(Number(seg))
                 : srcParent.get(seg);

               if (!(nextSrcParent instanceof Y.Map || nextSrcParent instanceof Y.Array)) {
                 throw new Error(`Invalid or non-existent source container at segment '${seg}' in path '${fromPath}'`);
               }
               srcParent = nextSrcParent;
            }

            const srcKey = srcParent instanceof Y.Array
              ? Number(fromKeySegment)
              : fromKeySegment;

            let valueToMove;
            if (srcParent instanceof Y.Array) {
              if (srcKey >= srcParent.length || srcKey < 0) {
                throw new Error(`Source index ${srcKey} out of bounds in array at path '${srcParentPathSegments.join('/')}'`);
              }
              valueToMove = srcParent.get(srcKey); // Get value before removing
              if (valueToMove instanceof Y.AbstractType) {
                valueToMove = valueToMove.toJSON();
              }
              srcParent.delete(srcKey, 1); // Remove from source
            } else if (srcParent instanceof Y.Map) {
                if (!srcParent.has(srcKey)) {
                  throw new Error(`Source key '${srcKey}' not found in map at path '${srcParentPathSegments.join('/')}'`);
                }
                valueToMove = srcParent.get(srcKey); // Get value before removing
                if (valueToMove instanceof Y.AbstractType) {
                valueToMove = valueToMove.toJSON();
              }
                srcParent.delete(srcKey); // Remove from source
            } else {
                throw new Error(`Invalid source parent type ${typeof srcParent} at path '${srcParentPathSegments.join('/')}'`);
            }

            // Insert the newly created Y structure at target
            if (parent instanceof Y.Array) {
               if (key > parent.length || key < 0) {
                throw new Error(`Target index ${key} out of bounds for move operation in array at path '${parentPathSegments.join('/')}'`);
              }
              // Add the value obtained from the source
              parent.insert(key, [this.createNestedYType(valueToMove)]);
            } else { // Map
              // Add the value obtained from the source
              parent.set(key, this.createNestedYType(valueToMove));
            }
            break;
          }
          case 'copy': {
            const fromPath = op.from;
            // Source navigation: path relative to root map
            const sourcePathSegments = fromPath.split('/').filter(s => s);
            const srcParentPathSegments = sourcePathSegments.slice(0, -1);
            const fromKeySegment = sourcePathSegments[sourcePathSegments.length - 1];

            let srcParent = rootMap;
             for (const seg of srcParentPathSegments) {
               const nextSrcParent = srcParent instanceof Y.Array
                 ? srcParent.get(Number(seg))
                 : srcParent.get(seg);

               if (!(nextSrcParent instanceof Y.Map || nextSrcParent instanceof Y.Array)) {
                 throw new Error(`Invalid or non-existent source container at segment '${seg}' in path '${fromPath}'`);
               }
               srcParent = nextSrcParent;
            }

            const srcKey = srcParent instanceof Y.Array
              ? Number(fromKeySegment)
              : fromKeySegment;

             // Check if source exists
             if (srcParent instanceof Y.Array) {
                if (srcKey >= srcParent.length || srcKey < 0) {
                  throw new Error(`Source index ${srcKey} out of bounds in array at path '${srcParentPathSegments.join('/')}'`);
                }
             } else if (srcParent instanceof Y.Map) {
                 if (!srcParent.has(srcKey)) {
                   throw new Error(`Source key '${srcKey}' not found in map at path '${srcParentPathSegments.join('/')}'`);
                 }
             } else {
                 throw new Error(`Invalid source parent type ${typeof srcParent} at path '${srcParentPathSegments.join('/')}'`);
             }

            const sourceYValue = srcParent.get(srcKey);

            // Convert source Yjs value to plain JS to copy structure and data
            const plainValue = sourceYValue instanceof Y.AbstractType
                 ? sourceYValue.toJSON()
                 : sourceYValue; // Handle primitives if they are directly stored

            // Create new nested Y structure from the plain JS value
            const yValueToCopy = this.createNestedYType(plainValue);

            // Insert the newly created Y structure at target
            if (parent instanceof Y.Array) {
               if (key > parent.length || key < 0) {
                throw new Error(`Target index ${key} out of bounds for copy operation in array at path '${parentPathSegments.join('/')}'`);
              }
              // Note: copy in an array behaves as an insert, not a replace, see RFC 6902 for details
              parent.insert(key, [yValueToCopy]);
            } else { // Map
              parent.set(key, yValueToCopy);
            }
            break;
          }
          default:
             console.warn(`Unsupported operation type: ${type}`);
            break;
        }
      }
    });
  }

  getState() {
    return this.toJSON();
  }

  /**
   * Synchronize the document to match the target JSON state.
   * Applies derived JSON Patch operations against the document.
   *
   * @param {object} newState - Target JSON state
   * @param {object} [options] - deriveOps options (e.g., hashFunction)
   * @returns {Array} The list of operations applied
   */
  updateToState(newState, options = {}) {
    // Get current document state as JSON
    const currentState = this.getState();
    // Derive patch operations
    const ops = deriveOps(currentState, newState, options);
    // Apply operations to the document
    this.applyOperations(ops);
    return ops;
  }
}