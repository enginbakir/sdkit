// Runs the kit's guards (secrets, protected paths, frozen tests, prod kubectl, release-flow git) before OpenCode tool calls.
// A two-line shim: it hands the call to `sdkit hook --host opencode PreToolUse` (argv array, no shell string) and turns a
// deny/ask answer into a thrown error. No decision means the call is allowed.
// UNVERIFIED against a live OpenCode: tool names (read/edit/write/patch/shell|bash) and whether hooks also fire inside
// subagents; on Windows also whether `sdkit` is found on PATH (unverified until the Windows CI run). Git hooks (.githooks) remain the backstop.
import { spawnSync } from "node:child_process"

const TOOLS = { read: "Read", write: "Write", edit: "Edit", patch: "Edit", shell: "Bash", bash: "Bash" }

export const SpecDrivenKitGuard = async ({ directory }) => ({
  "tool.execute.before": async (input, output) => {
    const tool = TOOLS[input.tool]
    if (!tool) return
    const args = output.args || {}
    const payload = JSON.stringify({ tool_name: tool, tool_input: { file_path: args.filePath || args.file_path || args.path || "", command: args.command || "" } })
    const res = spawnSync("sdkit", ["hook", "--host", "opencode", "PreToolUse"], { input: payload, env: { ...process.env, CLAUDE_PROJECT_DIR: directory }, encoding: "utf8" })
    let decision
    try { decision = JSON.parse(res.stdout || "{}").hookSpecificOutput } catch { decision = undefined }
    if (decision && (decision.permissionDecision === "deny" || decision.permissionDecision === "ask")) {
      const prefix = decision.permissionDecision === "ask" ? "needs explicit approval: " : ""
      throw new Error(prefix + (decision.permissionDecisionReason || "blocked by spec-driven-kit"))
    }
  },
})
