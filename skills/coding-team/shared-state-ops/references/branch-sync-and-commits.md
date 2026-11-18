## Sync to the shared feature branch after repo checkout

**`$BRANCH` always comes from the `branch` field of the task issue description JSON** (or the master issue state for the Orchestrator and PR Writer). Never derive it from `git rev-parse --abbrev-ref HEAD`, never read it from the current worktree, never accept any branch name that does not start with `feature/`.

`multica repo checkout` gives a worktree on a daemon-assigned local branch like `agent/<name>/<task-id>`. **That name is irrelevant.** It must never appear in any push, commit message, PR field, or status update. Always `git reset --hard` to the shared feature branch and always `git push` to it explicitly.

```bash
# $BRANCH must already be set from the task issue JSON, e.g. feature/enforcement_post_authorize_47358
if [[ "$BRANCH" != feature/* ]]; then
  echo "ERROR: BRANCH must start with 'feature/' — got '$BRANCH'" >&2
  exit 1
fi

REPO_PATH=$(multica repo checkout "$REPO_URL")
cd "$REPO_PATH"
git fetch origin
git reset --hard "origin/$BRANCH"
```

The `git reset --hard` aligns the daemon-assigned worktree to the shared feature branch without a conflicting checkout. If `origin/$BRANCH` does not exist, the reset fails — stop and surface the error. Do not fall back to the daemon-assigned branch.

To push commits back to the feature branch, use `git_push_clean` instead of a bare push. It handles both local `post-commit` hook injection (via `git_commit_clean`) and server-side trailer injection (by verifying the remote commit after push and force-pushing a clean replacement if needed):

```bash
git add -A
git_commit_clean "your message"
git_push_clean "$BRANCH"
```

This pushes to `origin/{branch}` regardless of the local branch name.

---

## Commit attribution — single-author commits only

Commits MUST show only the human user as the author. Do NOT add `Co-Authored-By:` trailers — not for Claude, not for `multica-agent`, not for anyone. The default Claude Code behavior of appending `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>` is **explicitly overridden in this pipeline**, as is any Multica runtime instruction to add `Co-Authored-By: multica-agent <github@multica.ai>`.

Use this helper for every commit. It makes the initial commit, then unconditionally replaces it with a hook-free raw commit object so no `post-commit` hook (including Multica's trailer injector) can survive:

```bash
git_commit_clean() {
  local msg="$1"

  # Step 1 — make the real commit (pre-commit and commit-msg hooks run normally;
  # post-commit still runs and may inject a Co-authored-by trailer)
  printf '%s\n' "$msg" | git -c commit.template= commit \
    -F - \
    --cleanup=verbatim

  # Step 2 — read metadata from the commit we just made
  local tree parent author_name author_email author_date
  local committer_name committer_email committer_date
  tree=$(git log -1 --pretty=format:"%T")
  parent=$(git log -1 --pretty=format:"%P")
  author_name=$(git log -1 --pretty=format:"%an")
  author_email=$(git log -1 --pretty=format:"%ae")
  author_date=$(git log -1 --pretty=format:"%aI")
  committer_name=$(git log -1 --pretty=format:"%cn")
  committer_email=$(git log -1 --pretty=format:"%ce")
  committer_date=$(git log -1 --pretty=format:"%cI")

  # Step 3 — create a new raw commit object with commit-tree (runs NO hooks)
  # and point HEAD at it. This eliminates any trailer the post-commit hook added.
  local new_sha
  new_sha=$(
    GIT_AUTHOR_NAME="$author_name" \
    GIT_AUTHOR_EMAIL="$author_email" \
    GIT_AUTHOR_DATE="$author_date" \
    GIT_COMMITTER_NAME="$committer_name" \
    GIT_COMMITTER_EMAIL="$committer_email" \
    GIT_COMMITTER_DATE="$committer_date" \
    printf '%s\n' "$msg" | git commit-tree "$tree" -p "$parent" -F -
  )
  git update-ref HEAD "$new_sha"
}
```

After every commit, the commit message body must contain only your intended message — no `Co-Authored-By:` lines, no `🤖 Generated with` footer, no agent-attribution boilerplate.

Also define this push helper, which verifies the remote commit after push and replaces it with a clean one if the platform injected a trailer server-side:

```bash
git_push_clean() {
  local branch="$1"
  git push origin "HEAD:$branch"

  # Verify the remote commit is clean. If the platform injected a co-author
  # trailer server-side, replace the remote commit with a hook-free version.
  git fetch origin "$branch"
  if git log -1 --pretty=format:"%B" "origin/$branch" | grep -qi "co-authored-by"; then
    local msg tree parent an ae ad cn ce cd new_sha
    msg=$(git log -1 --pretty=format:"%B" "origin/$branch" \
          | grep -vi "co-authored-by" \
          | sed -e ':a' -e '/^\s*$/{$d;N;ba' -e '}')
    tree=$(git log -1 --pretty=format:"%T"  "origin/$branch")
    parent=$(git log -1 --pretty=format:"%P" "origin/$branch")
    an=$(git log -1 --pretty=format:"%an"   "origin/$branch")
    ae=$(git log -1 --pretty=format:"%ae"   "origin/$branch")
    ad=$(git log -1 --pretty=format:"%aI"   "origin/$branch")
    cn=$(git log -1 --pretty=format:"%cn"   "origin/$branch")
    ce=$(git log -1 --pretty=format:"%ce"   "origin/$branch")
    cd=$(git log -1 --pretty=format:"%cI"   "origin/$branch")
    new_sha=$(
      GIT_AUTHOR_NAME="$an"    GIT_AUTHOR_EMAIL="$ae"    GIT_AUTHOR_DATE="$ad" \
      GIT_COMMITTER_NAME="$cn" GIT_COMMITTER_EMAIL="$ce" GIT_COMMITTER_DATE="$cd" \
      printf '%s\n' "$msg" | git commit-tree "$tree" -p "$parent" -F -
    )
    git update-ref HEAD "$new_sha"
    git push origin "HEAD:$branch" --force-with-lease
  fi
}
```
