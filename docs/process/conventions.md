# Process conventions

## Develop every new feature in a git worktree

Never switch branches in the main checkout to start a feature. Several
coding sessions may run on this repo at once. They share the main checkout,
so a `git checkout` in one session changes the files under the other.

For each new feature:

1. Create a worktree for the feature branch under `.claude/worktrees/`:

   ```bash
   git worktree add .claude/worktrees/<feature> -b feat/<feature> master
   ```

   `.claude/worktrees/` must be ignored. It is listed in
   `.git/info/exclude`, so the ignore does not depend on which branch the
   main checkout has.

2. Work, test and commit only inside that worktree.
3. After the branch is merged, remove the worktree:

   ```bash
   git worktree remove .claude/worktrees/<feature>
   ```

Do not use a bare `git stash`. The stash stack is shared by all worktrees.
