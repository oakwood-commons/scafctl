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
 * Consent (asking the user before committing) is handled separately by
 * the `permission` block in opencode.json, which also denies `git push` and
 * `git commit --amend`; this hook only enforces the signing-key precondition.
 */
export const GitSigningGuard: Plugin = async ({ $ }) => {
  return {
    "tool.execute.before": async (input, output) => {
      if (input.tool !== "bash") return

      const command: string = output.args?.command ?? ""
      // Only care about commit-creating commands.
      const isCommit = /\bgit\b[^&|;]*\bcommit\b/.test(command)
      if (!isCommit) return

      // Enumerate identities currently loaded in the ssh-agent.
      // exit 0 => at least one key; 1 => agent has no identities;
      // 2 => cannot contact agent.
      let agentList = ""
      let agentHasAnyKey = false
      try {
        const res = await $`ssh-add -l`.quiet().nothrow()
        agentHasAnyKey = res.exitCode === 0
        agentList = res.stdout?.toString() ?? ""
      } catch {
        agentHasAnyKey = false
      }

      // Resolve the repo's configured SSH signing key fingerprint so we can
      // verify THAT specific key is loaded -- not just any unrelated identity.
      // If gpg.format is not ssh, or the key/ssh-keygen is unavailable, we fall
      // back to the weaker "any key loaded" check rather than blocking blindly.
      let signingKeyFingerprint = ""
      try {
        const fmt = (
          await $`git config --get gpg.format`.quiet().nothrow()
        ).stdout
          ?.toString()
          .trim()
        if (fmt === "ssh") {
          const keyPath = (
            await $`git config --get user.signingkey`.quiet().nothrow()
          ).stdout
            ?.toString()
            .trim()
          if (keyPath) {
            const fp = (
              await $`ssh-keygen -lf ${keyPath}`.quiet().nothrow()
            ).stdout
              ?.toString()
              .trim()
            // `ssh-keygen -lf` output: "<bits> SHA256:<hash> <comment> (<type>)"
            const match = fp?.match(/SHA256:[A-Za-z0-9+/]+/)
            if (match) signingKeyFingerprint = match[0]
          }
        }
      } catch {
        signingKeyFingerprint = ""
      }

      // Prefer the precise check: is the configured signing key loaded?
      // Otherwise degrade to "is any key loaded?".
      const hasKey = signingKeyFingerprint
        ? agentList.includes(signingKeyFingerprint)
        : agentHasAnyKey

      if (!hasKey) {
        throw new Error(
          [
            "Commit blocked: the SSH signing key required by this repo is not",
            "loaded in the ssh-agent. Signed commits will fail without it.",
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
