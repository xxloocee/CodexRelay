import { Call } from "/wails/runtime.js";

// Named calls avoid coupling maintenance actions to generated numeric IDs.
const call = (name, ...args) => Call.ByName(`codexrelay/internal/desktop.DesktopService.${name}`, ...args);
export const GetCodexDiagnostics = () => call("GetCodexDiagnostics");
export const PreviewCodexHistoryRepair = (source) => call("PreviewCodexHistoryRepair", source);
export const ApplyCodexHistoryRepair = (source, token) => call("ApplyCodexHistoryRepair", source, token);
export const RestoreCodexHistoryRepair = (backupID) => call("RestoreCodexHistoryRepair", backupID);
