import type { Plugin } from "@opencode-ai/plugin"

/**
 * git-signing-guard
 *
 * This repo requires every commit to be BOTH:
 *   - SSH/GPG signed (repo has commit.gpgsign=true), and
 *   - DCO signed-off (`-s`, enforced by the DCO GitHub check).
 *
 * The SSH signing key here (~/.ssh/id_ecdsa_public_github) is passphrase
 * protected. If it is not loaded into the ssh-agent, `git commit` fails
 * mid-operation with a cryptic passphrase error and no commit object is
 * written.
 *
 * This hook intercepts `git commit` before it runs and, when a signing key is
 * not available in the agent, blocks the command with actionable instructions
 * instead of letting it fail obscurely. It also nudges toward `-s` (DCO).
 *
 * Consent (asking the user before committing/pushing) is handled separately by
 * the `permission` block in opencode.json; this hook only enforces the
 * signing-key precondition.
 */
export const GitSigningGuard: Plugin = async ({ $ }) => {
  return {
    "tool.execute.before": async (input, output) => {
      if (input.tool !== "bash") return

      const command: string = output.args?.command ?? ""
      // Only care about commit-creating commands.
      const isCommit = /\bgit\b[^&|;]*\bcommit\b/.test(command)
      if (!isCommit) return

      // Is any identity loaded in the ssh-agent?
      let hasKey = false
      try {
        const res = await $`ssh-add -l`.quiet().nothrow()
        // exit 0 => at least one key; 1 => agent has no identities;
        // 2 => cannot contact agent.
        hasKey = res.exitCode === 0
      } catch {
        hasKey = false
      }

      if (!hasKey) {
        throw new Error(
          [
            "Commit blocked: no SSH signing key is loaded in the ssh-agent.",
            "This repo requires signed commits, which will fail without the key.",
            "",
            "Load the signing key (you will be prompted for its passphrase):",
            "",
            "    ssh-add --apple-use-keychain ~/.ssh/id_ecdsa_public_github",
            "",
            "(On non-macOS hosts, drop the --apple-use-keychain flag.)",
            "",
            "Verify it is loaded, then retry the commit:",
            "",
            "    ssh-add -l",
            "",
            "Note: for repos under the Public/ folder, sign as abaker9@gmail.com",
            "using ~/.ssh/id_ecdsa_public_github(.pub).",
          ].join("\n"),
        )
      }

      // Signing key is present. Encourage DCO sign-off if it is missing and
      // not already supplied via a here-doc/message body.
      const hasSignoff = /(^|\s)(-s|--signoff)(\s|$)/.test(command)
      if (!hasSignoff) {
        // Do not hard-block: the message body may already contain a
        // Signed-off-by trailer. Just surface a reminder in logs.
        console.warn(
          "git-signing-guard: commit has no explicit -s/--signoff flag; " +
            "ensure a 'Signed-off-by:' trailer is present for the DCO check.",
        )
      }
    },
  }
}
