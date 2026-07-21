import type { Plugin } from "@opencode-ai/plugin"

/**
 * publish-consent-guard
 *
 * Enforces the repo rule that publishing actions -- creating commits, pushing,
 * and opening/merging PRs -- require explicit user approval. This is a
 * defense-in-depth backstop for the `permission` block in opencode.json: the
 * permission "ask" rules can be bypassed by argument-shape mismatches, so this
 * hook blocks the command unconditionally and forces the agent to obtain
 * approval through the `question`/ask flow before retrying.
 *
 * Approval model (per the user's directive): a SINGLE approval may cover the
 * whole commit -> push -> PR sequence. Once the user approves a publish action
 * in a session, the agent sets the env var PUBLISH_APPROVED=1 (via the shell)
 * for the subsequent chained commands. Absent that marker, the command is
 * blocked with instructions to ask first.
 *
 * Blocked without approval:
 *   - git push ...
 *   - gh pr create ...
 *   - gh pr merge ...
 *
 * NOT blocked here (handled by git-signing-guard + permission block): plain
 * `git commit` still goes through the opencode permission "ask" rule and the
 * signing-key precondition.
 */
export const PublishConsentGuard: Plugin = async () => {
  return {
    "tool.execute.before": async (input, output) => {
      if (input.tool !== "bash") return

      const command: string = output.args?.command ?? ""

      const isPush = /\bgit\b[^&|;]*\bpush\b/.test(command)
      const isPrCreate = /\bgh\b[^&|;]*\bpr\b[^&|;]*\bcreate\b/.test(command)
      const isPrMerge = /\bgh\b[^&|;]*\bpr\b[^&|;]*\bmerge\b/.test(command)

      if (!isPush && !isPrCreate && !isPrMerge) return

      // The agent records a granted approval by prefixing the command with
      // PUBLISH_APPROVED=1 (an explicit, auditable in-band marker the user
      // can see in the command being run).
      const hasApproval = /(^|\s|;|&&)\s*PUBLISH_APPROVED=1\b/.test(command)
      if (hasApproval) return

      const action = isPush ? "push" : isPrCreate ? "open a PR" : "merge a PR"
      throw new Error(
        [
          `Blocked: attempting to ${action} without recorded user approval.`,
          "",
          "Publishing (commit + push + PR) requires the user's explicit",
          "approval FIRST. Use the question/ask flow to get approval, then",
          "re-run the command prefixed with PUBLISH_APPROVED=1 so the approval",
          "is visible and auditable, e.g.:",
          "",
          "    PUBLISH_APPROVED=1 git push -u origin <branch>",
          "    PUBLISH_APPROVED=1 gh pr create --base main --head <branch> ...",
          "",
          "A single approval may cover the whole commit -> push -> PR sequence.",
        ].join("\n"),
      )
    },
  }
}
