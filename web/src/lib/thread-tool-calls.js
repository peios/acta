/** @typedef {import('./threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {{kind:'tool-call', id:string, started_at?:string, completed_at?:string|null, frame:ThreadFrame, toolId:string, turn:string, data:Record<string,unknown>, argumentsText:string, output:string, status:string, interruptedByTurn:boolean}} ToolCallItem */
/** @param {string} status */
export const terminalTool = (status) =>
  [
    "completed",
    "failed",
    "interrupted",
    "declined",
    "permission_denied",
  ].includes(status);
