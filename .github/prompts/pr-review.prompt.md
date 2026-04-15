---
description: "scafctl: Fetch and triage PR review comments and CI failures for the current branch. Presents findings for approval before handing off to go-fixer."
agent: "pr-reviewer"
argument-hint: "Optional: PR number or leave blank to use current branch"
---
Triage unresolved PR review comments and CI failures. Use `gh` CLI and the **GitHub GraphQL API (v4)** to fetch review threads -- the REST API does not expose the `isResolved` field.

Follow these phases **in order** -- do not skip ahead:

1. **Fetch**: Fetch all review threads via GraphQL; **skip comments that are already resolved**. Include **outdated but unresolved** threads -- these still need a response and resolution even if the code has moved
2. **Pipeline check**: Run `gh pr checks <PR_NUMBER>` to see CI status. If any checks are failing, run `gh run view <RUN_ID> --log-failed` to get failure logs. Include pipeline failures in the triage summary alongside review comments -- they may overlap with reviewer feedback or reveal additional issues
3. **Coverage check**: Assess patch coverage from Codecov:
   - First, check the PR comments for a Codecov report: `gh pr view <PR_NUMBER> --json comments --jq '.comments[] | select(.body | contains("Codecov")) | .body'`
   - The PR comment only shows the top 10 files. To get the **full file list**, fetch the Codecov tree view: `https://app.codecov.io/gh/oakwood-commons/scafctl/pull/<PR_NUMBER>?dropdown=coverage&src=pr&el=tree`
   - Present a **sorted table** of all files with missed lines > 0, including: file path, missed lines, head %, patch %, and change %
   - Flag any files with **0% patch coverage** -- these are the highest priority
   - Note the overall **patch coverage %** and whether it meets the **70% target**
   - If patch coverage is below 70%, list the top files where adding tests would have the most impact (most missed lines)
4. **Early exit**: If there are **zero unresolved threads**, **all checks are passing**, and **patch coverage >= 70%**, report that and stop
5. **Triage**: For each unresolved comment, pipeline failure, and coverage gap, assess whether it's a legit problem with the code. Present the triage summary with recommendations and **stop here** -- the user will click "Apply fixes" to hand off to the fixer agent

Include thread IDs in the triage output so the fixer agent can respond to and resolve them after applying fixes.
