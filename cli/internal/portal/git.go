package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Git runs the developer's git in the app directory for the portal's Git
// screen (ADR-0076). It never embeds a git implementation: hooks, signing,
// credentials and configuration behave as in the terminal. It never
// rewrites history, force-pushes or deletes what a plain command would
// refuse to; the frontend confirms every destructive action with what it
// loses, and this side reports that.
type Git struct {
	dir     string
	timeout time.Duration
	run     func(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, []byte, error)
}

// GitTimeout bounds a git command; network commands (fetch, pull, push)
// get GitNetworkTimeout.
const (
	GitTimeout        = 30 * time.Second
	GitNetworkTimeout = 2 * time.Minute
)

// ErrNotARepository reports a directory git doesn't manage.
var ErrNotARepository = errors.New("not a git repository")

// gitError is git's own refusal, with its message for the developer.
type gitError struct {
	args   []string
	stderr string
	code   int
}

func (e *gitError) Error() string {
	msg := strings.TrimSpace(e.stderr)
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", e.code)
	}
	return "git " + strings.Join(e.args, " ") + ": " + msg
}

// NewGit runs git in dir.
func NewGit(dir string) *Git {
	return &Git{dir: dir, timeout: GitTimeout, run: runGit}
}

func runGit(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // arguments are validated (paths, branch names) and never pass through a shell
	cmd.Dir = dir
	// No prompts: a credential helper or an SSH agent answers, or the
	// command fails with its message.
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_PAGER=cat", "LC_ALL=C")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// git runs one command and returns its output; a failing command becomes
// a *gitError.
func (g *Git) git(ctx context.Context, timeout time.Duration, stdin []byte, args ...string) (string, error) {
	out, _, err := g.gitBoth(ctx, timeout, stdin, args...)
	return out, err
}

// gitBoth is git with stderr returned too, for the commands whose
// summary goes there (fetch, push).
func (g *Git) gitBoth(ctx context.Context, timeout time.Duration, stdin []byte, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, errOut, err := g.run(ctx, g.dir, stdin, args...)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return string(out), string(errOut), &gitError{args: args, stderr: string(errOut), code: exit.ExitCode()}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return string(out), string(errOut), fmt.Errorf("git %s: took longer than %s", strings.Join(args, " "), timeout)
		}
		return string(out), string(errOut), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), string(errOut), nil
}

// Available reports whether dir is inside a git repository.
func (g *Git) Available(ctx context.Context) bool {
	out, err := g.git(ctx, g.timeout, nil, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// GitStatus is the repository at a glance.
type GitStatus struct {
	Branch string `json:"branch"`
	// Detached reports HEAD not on a branch; Branch is then the short hash.
	Detached bool   `json:"detached"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// Files are the changed files: staged, unstaged, untracked, conflicts.
	Files []GitFile `json:"files"`
	// State is the operation in progress: merging, rebasing, cherry-picking
	// or "" (clean).
	State  string `json:"state,omitempty"`
	Head   string `json:"head"`
	Remote string `json:"remote,omitempty"`
	// Summary counts.
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
	Conflicts int `json:"conflicts"`
}

// GitFile is one changed path.
type GitFile struct {
	Path string `json:"path"`
	// OldPath is set for renames.
	OldPath string `json:"old_path,omitempty"`
	// Index and Worktree are git's two status letters (M, A, D, R, ?, U…).
	Index     string `json:"index"`
	Worktree  string `json:"worktree"`
	Staged    bool   `json:"staged"`
	Unstaged  bool   `json:"unstaged"`
	Untracked bool   `json:"untracked"`
	Conflict  bool   `json:"conflict"`
}

// Status reads git status --porcelain=v2 --branch.
func (g *Git) Status(ctx context.Context) (GitStatus, error) {
	out, err := g.git(ctx, g.timeout, nil, "status", "--porcelain=v2", "--branch", "--untracked-files=all")
	if err != nil {
		return GitStatus{}, err
	}
	st := GitStatus{Files: []GitFile{}}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			st.Head = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
			if st.Branch == "(detached)" {
				st.Detached = true
				if len(st.Head) >= 7 {
					st.Branch = st.Head[:7]
				}
			}
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
			if i := strings.IndexByte(st.Upstream, '/'); i > 0 {
				st.Remote = st.Upstream[:i]
			}
		case strings.HasPrefix(line, "# branch.ab "):
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &st.Ahead, &st.Behind)
		case strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 "):
			f := parsePorcelainEntry(line)
			st.Files = append(st.Files, f)
		case strings.HasPrefix(line, "u "):
			fields := strings.SplitN(line, " ", 11)
			if len(fields) == 11 {
				st.Files = append(st.Files, GitFile{Path: fields[10], Index: fields[1][:1], Worktree: fields[1][1:], Conflict: true, Unstaged: true})
			}
		case strings.HasPrefix(line, "? "):
			st.Files = append(st.Files, GitFile{Path: strings.TrimPrefix(line, "? "), Index: "?", Worktree: "?", Untracked: true})
		}
	}
	for _, f := range st.Files {
		switch {
		case f.Conflict:
			st.Conflicts++
		case f.Untracked:
			st.Untracked++
		default:
			if f.Staged {
				st.Staged++
			}
			if f.Unstaged {
				st.Unstaged++
			}
		}
	}
	st.State = g.state(ctx)
	return st, nil
}

// parsePorcelainEntry reads a "1 XY …" or "2 XY … path\torig" line.
func parsePorcelainEntry(line string) GitFile {
	f := GitFile{}
	if strings.HasPrefix(line, "1 ") {
		fields := strings.SplitN(line, " ", 9)
		if len(fields) == 9 {
			f.Path = fields[8]
			f.Index, f.Worktree = fields[1][:1], fields[1][1:]
		}
	} else {
		fields := strings.SplitN(line, " ", 10)
		if len(fields) == 10 {
			paths := strings.SplitN(fields[9], "\t", 2)
			f.Path = paths[0]
			if len(paths) == 2 {
				f.OldPath = paths[1]
			}
			f.Index, f.Worktree = fields[1][:1], fields[1][1:]
		}
	}
	f.Staged = f.Index != "." && f.Index != ""
	f.Unstaged = f.Worktree != "." && f.Worktree != ""
	return f
}

// state reports an operation in progress from .git.
func (g *Git) state(ctx context.Context) string {
	dir, err := g.git(ctx, g.timeout, nil, "rev-parse", "--git-dir")
	if err != nil {
		return ""
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(g.dir, dir)
	}
	for _, c := range []struct{ file, state string }{{"MERGE_HEAD", "merging"}, {"rebase-merge", "rebasing"}, {"rebase-apply", "rebasing"}, {"CHERRY_PICK_HEAD", "cherry-picking"}, {"REVERT_HEAD", "reverting"}} {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.state
		}
	}
	return ""
}

// GitDiff is one file's diff.
type GitDiff struct {
	Path   string `json:"path"`
	Staged bool   `json:"staged"`
	// Patch is the unified diff; Binary reports a file git won't diff.
	Patch  string `json:"patch"`
	Binary bool   `json:"binary"`
	// Untracked files are shown whole, as an addition.
	Untracked bool `json:"untracked"`
	Additions int  `json:"additions"`
	Deletions int  `json:"deletions"`
}

// Diff returns a file's diff: the index against the worktree, or with
// staged, HEAD against the index.
func (g *Git) Diff(ctx context.Context, path string, staged bool) (GitDiff, error) {
	if err := validRepoPath(path); err != nil {
		return GitDiff{}, err
	}
	d := GitDiff{Path: path, Staged: staged}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--"}
	if staged {
		args = []string{"diff", "--cached", "--no-color", "--no-ext-diff", "--"}
	}
	out, err := g.git(ctx, g.timeout, nil, append(args, path)...)
	if err != nil {
		return GitDiff{}, err
	}
	if out == "" && !staged {
		// Untracked: show the whole file as new.
		if tracked, _ := g.git(ctx, g.timeout, nil, "ls-files", "--error-unmatch", "--", path); tracked == "" {
			out, err = g.git(ctx, g.timeout, nil, "diff", "--no-color", "--no-index", "--", "/dev/null", path)
			var ge *gitError
			if err != nil && (!errors.As(err, &ge) || ge.code != 1) {
				return GitDiff{}, err
			}
			d.Untracked = true
		}
	}
	d.Patch = out
	d.Binary = strings.Contains(out, "Binary files") || strings.Contains(out, "GIT binary patch")
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			d.Additions++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			d.Deletions++
		}
	}
	return d, nil
}

// Stage adds paths to the index (untracked ones too); Unstage removes them
// from it, keeping the changes.
func (g *Git) Stage(ctx context.Context, paths []string) error {
	if err := validRepoPaths(paths); err != nil {
		return err
	}
	_, err := g.git(ctx, g.timeout, nil, append([]string{"add", "--"}, paths...)...)
	return err
}

// Unstage removes paths from the index, keeping the working tree.
func (g *Git) Unstage(ctx context.Context, paths []string) error {
	if err := validRepoPaths(paths); err != nil {
		return err
	}
	_, err := g.git(ctx, g.timeout, nil, append([]string{"restore", "--staged", "--"}, paths...)...)
	return err
}

// ApplyPatch stages (or with reverse, unstages) a patch of hunks, as the
// diff viewer's per-hunk buttons need: git apply --cached [-R].
func (g *Git) ApplyPatch(ctx context.Context, patch string, reverse bool) error {
	if strings.TrimSpace(patch) == "" {
		return errors.New("empty patch")
	}
	args := []string{"apply", "--cached", "--recount", "--unidiff-zero"}
	if reverse {
		args = append(args, "-R")
	}
	_, err := g.git(ctx, g.timeout, []byte(patch), append(args, "-")...)
	return err
}

// Discard throws away the working tree changes of paths (git restore) and
// deletes untracked ones (git clean). Destructive: the frontend confirms
// with the diff first.
func (g *Git) Discard(ctx context.Context, paths []string) error {
	if err := validRepoPaths(paths); err != nil {
		return err
	}
	var tracked, untracked []string
	for _, p := range paths {
		if out, _ := g.git(ctx, g.timeout, nil, "ls-files", "--error-unmatch", "--", p); strings.TrimSpace(out) != "" {
			tracked = append(tracked, p)
		} else {
			untracked = append(untracked, p)
		}
	}
	if len(tracked) > 0 {
		if _, err := g.git(ctx, g.timeout, nil, append([]string{"restore", "--worktree", "--staged", "--source=HEAD", "--"}, tracked...)...); err != nil {
			return err
		}
	}
	if len(untracked) > 0 {
		if _, err := g.git(ctx, g.timeout, nil, append([]string{"clean", "-f", "--"}, untracked...)...); err != nil {
			return err
		}
	}
	return nil
}

// GitCommit is one commit.
type GitCommit struct {
	Hash    string    `json:"hash"`
	Short   string    `json:"short"`
	Parents []string  `json:"parents"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Time    time.Time `json:"time"`
	Subject string    `json:"subject"`
	Body    string    `json:"body,omitempty"`
	Refs    []string  `json:"refs"`
}

// Commit records the index with message; nothing is staged or unstaged
// for it. It returns the new commit. An empty index is an error.
func (g *Git) Commit(ctx context.Context, message string) (GitCommit, error) {
	if strings.TrimSpace(message) == "" {
		return GitCommit{}, errors.New("a commit message is required")
	}
	if _, err := g.git(ctx, g.timeout, []byte(message), "commit", "--file=-", "--cleanup=strip"); err != nil {
		return GitCommit{}, err
	}
	commits, err := g.Log(ctx, GitLogOptions{Limit: 1})
	if err != nil || len(commits) == 0 {
		return GitCommit{}, err
	}
	return commits[0], nil
}

// GitBranch is one local branch.
type GitBranch struct {
	Name     string `json:"name"`
	Current  bool   `json:"current"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Head     string `json:"head"`
	Subject  string `json:"subject"`
	// Merged reports the branch merged into the current one, so deleting
	// it loses nothing.
	Merged bool `json:"merged"`
}

// Branches lists the local branches and the remote ones' names.
func (g *Git) Branches(ctx context.Context) ([]GitBranch, []string, error) {
	out, err := g.git(ctx, g.timeout, nil, "for-each-ref", "--format=%(refname:short)\x1f%(HEAD)\x1f%(upstream:short)\x1f%(upstream:track,nobracket)\x1f%(objectname:short)\x1f%(subject)", "refs/heads")
	if err != nil {
		return nil, nil, err
	}
	merged := map[string]bool{}
	if m, err := g.git(ctx, g.timeout, nil, "branch", "--format=%(refname:short)", "--merged"); err == nil {
		for _, name := range strings.Split(m, "\n") {
			merged[strings.TrimSpace(name)] = true
		}
	}
	branches := []GitBranch{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x1f")
		if len(f) < 6 {
			continue
		}
		b := GitBranch{Name: f[0], Current: f[1] == "*", Upstream: f[2], Head: f[4], Subject: f[5], Merged: merged[f[0]]}
		for _, part := range strings.Split(f[3], ", ") {
			if n, ok := strings.CutPrefix(part, "ahead "); ok {
				b.Ahead, _ = strconv.Atoi(n)
			}
			if n, ok := strings.CutPrefix(part, "behind "); ok {
				b.Behind, _ = strconv.Atoi(n)
			}
		}
		branches = append(branches, b)
	}
	remote, _ := g.git(ctx, g.timeout, nil, "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	remotes := []string{}
	for _, name := range strings.Split(strings.TrimRight(remote, "\n"), "\n") {
		if name != "" && !strings.HasSuffix(name, "/HEAD") {
			remotes = append(remotes, name)
		}
	}
	return branches, remotes, nil
}

var branchNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)

// ValidBranchName reports a name git would accept (checked with git too).
func ValidBranchName(name string) bool {
	return branchNamePattern.MatchString(name) && !strings.Contains(name, "..") && !strings.HasSuffix(name, "/") && !strings.HasSuffix(name, ".lock") && !strings.Contains(name, "//")
}

// CreateBranch creates name from start (HEAD when empty) and switches to it
// when checkout.
func (g *Git) CreateBranch(ctx context.Context, name, start string, checkout bool) error {
	if !ValidBranchName(name) {
		return errors.New("not a valid branch name")
	}
	if start != "" && !ValidBranchName(start) && !isHash(start) {
		return errors.New("not a valid start point")
	}
	if _, err := g.git(ctx, g.timeout, nil, "check-ref-format", "--branch", name); err != nil {
		return errors.New("not a valid branch name")
	}
	args := []string{"branch", "--", name}
	if start != "" {
		args = append(args, start)
	}
	if _, err := g.git(ctx, g.timeout, nil, args...); err != nil {
		return err
	}
	if checkout {
		return g.Switch(ctx, name)
	}
	return nil
}

func isHash(s string) bool {
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// Switch checks out a branch; git refuses when local changes would be
// lost, and the message says so.
func (g *Git) Switch(ctx context.Context, name string) error {
	if !ValidBranchName(name) {
		return errors.New("not a valid branch name")
	}
	_, err := g.git(ctx, g.timeout, nil, "switch", "--", name)
	return err
}

// DeleteBranch deletes a local branch. Without force, git refuses an
// unmerged branch; the frontend shows what force would lose (the
// branch's commits not on the current branch) before offering it.
func (g *Git) DeleteBranch(ctx context.Context, name string, force bool) error {
	if !ValidBranchName(name) {
		return errors.New("not a valid branch name")
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := g.git(ctx, g.timeout, nil, "branch", flag, "--", name)
	return err
}

// UnmergedCommits are what deleting a branch would lose: its commits not
// reachable from HEAD.
func (g *Git) UnmergedCommits(ctx context.Context, name string) ([]GitCommit, error) {
	if !ValidBranchName(name) {
		return nil, errors.New("not a valid branch name")
	}
	return g.Log(ctx, GitLogOptions{Range: "HEAD.." + name, Limit: 100})
}

// GitRemoteResult is the answer to fetch, pull and push: git's output.
type GitRemoteResult struct {
	Output string `json:"output"`
	// Conflicts are the files a pull left conflicted.
	Conflicts []string `json:"conflicts,omitempty"`
}

// Fetch updates the remotes (fetch --all --prune).
func (g *Git) Fetch(ctx context.Context) (GitRemoteResult, error) {
	out, errOut, err := g.gitBoth(ctx, GitNetworkTimeout, nil, "fetch", "--all", "--prune")
	return GitRemoteResult{Output: strings.TrimSpace(out + "\n" + errOut)}, err
}

// Pull fetches and merges the upstream: a fast-forward when it can, a
// merge commit otherwise (never a rebase). Conflicts are listed.
func (g *Git) Pull(ctx context.Context) (GitRemoteResult, error) {
	out, err := g.git(ctx, GitNetworkTimeout, nil, "pull", "--no-rebase", "--no-edit")
	res := GitRemoteResult{Output: out}
	if err != nil {
		var ge *gitError
		if errors.As(err, &ge) {
			res.Output += ge.stderr
			res.Conflicts, _ = g.Conflicts(ctx)
			if len(res.Conflicts) > 0 {
				return res, nil
			}
		}
	}
	return res, err
}

// Push pushes the current branch, setting the upstream when it has none.
// Never with force.
func (g *Git) Push(ctx context.Context) (GitRemoteResult, error) {
	st, err := g.Status(ctx)
	if err != nil {
		return GitRemoteResult{}, err
	}
	if st.Detached {
		return GitRemoteResult{}, errors.New("HEAD is detached: switch to a branch first")
	}
	args := []string{"push"}
	if st.Upstream == "" {
		remote := st.Remote
		if remote == "" {
			remote = "origin"
		}
		args = append(args, "--set-upstream", remote, st.Branch)
	}
	out, errOut, err := g.gitBoth(ctx, GitNetworkTimeout, nil, args...)
	return GitRemoteResult{Output: strings.TrimSpace(out + "\n" + errOut)}, err
}

// GitMergePreview says what merging a branch would do, without touching
// the working tree (git merge-tree --write-tree, Git 2.38+).
type GitMergePreview struct {
	Branch string `json:"branch"`
	// Commits are the branch's commits not on HEAD.
	Commits []GitCommit `json:"commits"`
	// FastForward reports HEAD is an ancestor of the branch.
	FastForward bool `json:"fast_forward"`
	// UpToDate reports nothing to merge.
	UpToDate bool `json:"up_to_date"`
	// Conflicts are the files a merge would leave conflicted.
	Conflicts []string `json:"conflicts"`
	// Files are the paths the merge changes.
	Files []string `json:"files"`
}

// MergePreview previews merging branch into HEAD.
func (g *Git) MergePreview(ctx context.Context, branch string) (GitMergePreview, error) {
	if !ValidBranchName(branch) {
		return GitMergePreview{}, errors.New("not a valid branch name")
	}
	p := GitMergePreview{Branch: branch, Conflicts: []string{}, Files: []string{}}
	commits, err := g.Log(ctx, GitLogOptions{Range: "HEAD.." + branch, Limit: 200})
	if err != nil {
		return GitMergePreview{}, err
	}
	p.Commits = commits
	if len(commits) == 0 {
		p.UpToDate = true
		return p, nil
	}
	if _, err := g.git(ctx, g.timeout, nil, "merge-base", "--is-ancestor", "HEAD", branch); err == nil {
		p.FastForward = true
	}
	out, err := g.git(ctx, g.timeout, nil, "merge-tree", "--write-tree", "--name-only", "--no-messages", "HEAD", branch)
	var ge *gitError
	if err != nil && (!errors.As(err, &ge) || ge.code != 1) {
		return GitMergePreview{}, err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if ge != nil && ge.code == 1 && len(lines) > 1 { // conflicts: the tree, then the conflicted files
		p.Conflicts = lines[1:]
	}
	files, err := g.git(ctx, g.timeout, nil, "diff", "--name-only", "HEAD..."+branch)
	if err == nil {
		for _, f := range strings.Split(strings.TrimRight(files, "\n"), "\n") {
			if f != "" {
				p.Files = append(p.Files, f)
			}
		}
	}
	return p, nil
}

// Merge merges branch into HEAD (a fast-forward when possible, a merge
// commit otherwise). Conflicts are left in the working tree and listed.
func (g *Git) Merge(ctx context.Context, branch string) (GitRemoteResult, error) {
	if !ValidBranchName(branch) {
		return GitRemoteResult{}, errors.New("not a valid branch name")
	}
	out, err := g.git(ctx, g.timeout, nil, "merge", "--no-edit", "--", branch)
	res := GitRemoteResult{Output: out}
	if err != nil {
		var ge *gitError
		if errors.As(err, &ge) {
			res.Output += ge.stderr
			res.Conflicts, _ = g.Conflicts(ctx)
			if len(res.Conflicts) > 0 {
				return res, nil
			}
		}
	}
	return res, err
}

// AbortMerge returns to the state before a conflicted merge.
func (g *Git) AbortMerge(ctx context.Context) error {
	_, err := g.git(ctx, g.timeout, nil, "merge", "--abort")
	return err
}

// Conflicts lists the conflicted files.
func (g *Git) Conflicts(ctx context.Context) ([]string, error) {
	out, err := g.git(ctx, g.timeout, nil, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, f := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// GitLogOptions select commits.
type GitLogOptions struct {
	// Range is a revision range such as HEAD..feature; empty means HEAD.
	Range string
	// All includes every branch (the history graph).
	All   bool
	Limit int
	// Skip continues a page.
	Skip int
	// Path limits to commits touching it.
	Path string
}

// Log lists commits newest first with parents and refs, for the history
// graph.
func (g *Git) Log(ctx context.Context, opts GitLogOptions) ([]GitCommit, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 1000)
	args := []string{"log", "--topo-order", "--format=%H\x1f%h\x1f%P\x1f%an\x1f%ae\x1f%aI\x1f%s\x1f%b\x1f%D\x1e", "--max-count=" + strconv.Itoa(limit), "--skip=" + strconv.Itoa(max(opts.Skip, 0))}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Range != "" {
		if strings.HasPrefix(opts.Range, "-") {
			return nil, errors.New("not a revision range")
		}
		args = append(args, opts.Range)
	}
	if opts.Path != "" {
		if err := validRepoPath(opts.Path); err != nil {
			return nil, err
		}
		args = append(args, "--", opts.Path)
	}
	out, err := g.git(ctx, g.timeout, nil, args...)
	if err != nil {
		var ge *gitError
		if errors.As(err, &ge) && strings.Contains(ge.stderr, "does not have any commits") {
			return []GitCommit{}, nil
		}
		return nil, err
	}
	commits := []GitCommit{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.Split(rec, "\x1f")
		if len(f) < 9 {
			continue
		}
		c := GitCommit{Hash: f[0], Short: f[1], Parents: []string{}, Author: f[3], Email: f[4], Subject: f[6], Body: strings.TrimSpace(f[7]), Refs: []string{}}
		if f[2] != "" {
			c.Parents = strings.Fields(f[2])
		}
		c.Time, _ = time.Parse(time.RFC3339, f[5])
		for _, r := range strings.Split(f[8], ", ") {
			if r = strings.TrimSpace(r); r != "" {
				c.Refs = append(c.Refs, strings.TrimPrefix(r, "HEAD -> "))
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// validRepoPath refuses paths that leave the repository or look like
// options.
func validRepoPath(p string) error {
	if p == "" || strings.HasPrefix(p, "-") || strings.HasPrefix(p, "/") || strings.Contains(p, "\x00") {
		return errors.New("not a repository path")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return errors.New("not a repository path")
		}
	}
	return nil
}

func validRepoPaths(paths []string) error {
	if len(paths) == 0 {
		return errors.New("no paths")
	}
	for _, p := range paths {
		if err := validRepoPath(p); err != nil {
			return err
		}
	}
	return nil
}

// Git endpoints (ADR-0076), under /_portal/api/git; every write needs the
// mutation header, and the frontend confirms the destructive ones.
func (s *Server) gitRoutes(mux *http.ServeMux) {
	guard := func(fn func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Git == nil {
				writeProblem(w, http.StatusNotFound, "no_git", "this orb dev runs no git")
				return
			}
			if !s.cfg.Git.Available(r.Context()) {
				writeProblem(w, http.StatusNotFound, "not_a_repository", "the app directory isn't a git repository: run git init")
				return
			}
			fn(w, r)
		}
	}
	reply := func(w http.ResponseWriter, v any, err error) {
		if err != nil {
			writeGitError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusOK, v)
	}
	body := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_body", err.Error())
			return false
		}
		return true
	}
	mux.HandleFunc("GET "+APIPrefix+"git/status", guard(func(w http.ResponseWriter, r *http.Request) {
		st, err := s.cfg.Git.Status(r.Context())
		reply(w, st, err)
	}))
	mux.HandleFunc("GET "+APIPrefix+"git/diff", guard(func(w http.ResponseWriter, r *http.Request) {
		d, err := s.cfg.Git.Diff(r.Context(), r.URL.Query().Get("path"), r.URL.Query().Get("staged") == "true")
		reply(w, d, err)
	}))
	for _, op := range []struct {
		name string
		fn   func(ctx context.Context, paths []string) error
	}{{"stage", s.cfg.gitStage}, {"unstage", s.cfg.gitUnstage}, {"discard", s.cfg.gitDiscard}} {
		fn := op.fn
		mux.HandleFunc("POST "+APIPrefix+"git/"+op.name, guard(func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Paths []string `json:"paths"`
			}
			if !body(w, r, &in) {
				return
			}
			if err := fn(r.Context(), in.Paths); err != nil {
				writeGitError(w, err)
				return
			}
			st, err := s.cfg.Git.Status(r.Context())
			reply(w, st, err)
		}))
	}
	mux.HandleFunc("POST "+APIPrefix+"git/patch", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Patch   string `json:"patch"`
			Reverse bool   `json:"reverse"`
		}
		if !body(w, r, &in) {
			return
		}
		if err := s.cfg.Git.ApplyPatch(r.Context(), in.Patch, in.Reverse); err != nil {
			writeGitError(w, err)
			return
		}
		st, err := s.cfg.Git.Status(r.Context())
		reply(w, st, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/commit", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Message string `json:"message"`
		}
		if !body(w, r, &in) {
			return
		}
		c, err := s.cfg.Git.Commit(r.Context(), in.Message)
		reply(w, c, err)
	}))
	mux.HandleFunc("GET "+APIPrefix+"git/branches", guard(func(w http.ResponseWriter, r *http.Request) {
		local, remote, err := s.cfg.Git.Branches(r.Context())
		reply(w, map[string]any{"branches": local, "remote": remote}, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/branches", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name     string `json:"name"`
			From     string `json:"from"`
			Checkout bool   `json:"checkout"`
		}
		if !body(w, r, &in) {
			return
		}
		if err := s.cfg.Git.CreateBranch(r.Context(), in.Name, in.From, in.Checkout); err != nil {
			writeGitError(w, err)
			return
		}
		local, remote, err := s.cfg.Git.Branches(r.Context())
		reply(w, map[string]any{"branches": local, "remote": remote}, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/switch", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if !body(w, r, &in) {
			return
		}
		if err := s.cfg.Git.Switch(r.Context(), in.Name); err != nil {
			writeGitError(w, err)
			return
		}
		st, err := s.cfg.Git.Status(r.Context())
		reply(w, st, err)
	}))
	mux.HandleFunc("GET "+APIPrefix+"git/unmerged", guard(func(w http.ResponseWriter, r *http.Request) {
		branch := r.URL.Query().Get("branch")
		commits, err := s.cfg.Git.UnmergedCommits(r.Context(), branch)
		reply(w, map[string]any{"branch": branch, "commits": commits}, err)
	}))
	mux.HandleFunc("DELETE "+APIPrefix+"git/branches/{name...}", guard(func(w http.ResponseWriter, r *http.Request) {
		if err := s.cfg.Git.DeleteBranch(r.Context(), r.PathValue("name"), r.URL.Query().Get("force") == "true"); err != nil {
			writeGitError(w, err)
			return
		}
		local, remote, err := s.cfg.Git.Branches(r.Context())
		reply(w, map[string]any{"branches": local, "remote": remote}, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/fetch", guard(func(w http.ResponseWriter, r *http.Request) {
		res, err := s.cfg.Git.Fetch(r.Context())
		reply(w, res, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/pull", guard(func(w http.ResponseWriter, r *http.Request) {
		res, err := s.cfg.Git.Pull(r.Context())
		reply(w, res, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/push", guard(func(w http.ResponseWriter, r *http.Request) {
		res, err := s.cfg.Git.Push(r.Context())
		reply(w, res, err)
	}))
	mux.HandleFunc("GET "+APIPrefix+"git/merge/preview", guard(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.cfg.Git.MergePreview(r.Context(), r.URL.Query().Get("branch"))
		reply(w, p, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/merge", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Branch string `json:"branch"`
		}
		if !body(w, r, &in) {
			return
		}
		res, err := s.cfg.Git.Merge(r.Context(), in.Branch)
		reply(w, res, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/merge/abort", guard(func(w http.ResponseWriter, r *http.Request) {
		if err := s.cfg.Git.AbortMerge(r.Context()); err != nil {
			writeGitError(w, err)
			return
		}
		st, err := s.cfg.Git.Status(r.Context())
		reply(w, st, err)
	}))
	mux.HandleFunc("GET "+APIPrefix+"git/conflicts", guard(func(w http.ResponseWriter, r *http.Request) {
		files, err := s.cfg.Git.Conflicts(r.Context())
		reply(w, map[string]any{"files": files}, err)
	}))
	mux.HandleFunc("GET "+APIPrefix+"git/log", guard(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		skip, _ := strconv.Atoi(q.Get("skip"))
		commits, err := s.cfg.Git.Log(r.Context(), GitLogOptions{Range: q.Get("range"), All: q.Get("all") == "true", Limit: limit, Skip: skip, Path: q.Get("path")})
		reply(w, map[string]any{"commits": commits}, err)
	}))
	mux.HandleFunc("POST "+APIPrefix+"git/open", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
			Line int    `json:"line"`
		}
		if !body(w, r, &in) {
			return
		}
		if s.cfg.OpenInEditor == nil {
			writeProblem(w, http.StatusNotFound, "no_editor", "orb dev has no editor to open files with")
			return
		}
		if err := validRepoPath(in.Path); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_path", err.Error())
			return
		}
		if err := s.cfg.OpenInEditor(in.Path, in.Line); err != nil {
			writeProblem(w, http.StatusBadGateway, "editor_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

// Adapters so the loop above has one shape.
func (c Config) gitStage(ctx context.Context, paths []string) error { return c.Git.Stage(ctx, paths) }
func (c Config) gitUnstage(ctx context.Context, paths []string) error {
	return c.Git.Unstage(ctx, paths)
}
func (c Config) gitDiscard(ctx context.Context, paths []string) error {
	return c.Git.Discard(ctx, paths)
}

// writeGitError answers git's refusals as 409 with git's message, and
// bad input as 400.
func writeGitError(w http.ResponseWriter, err error) {
	var ge *gitError
	switch {
	case errors.As(err, &ge):
		writeProblem(w, http.StatusConflict, "git_refused", err.Error())
	case errors.Is(err, ErrNotARepository):
		writeProblem(w, http.StatusNotFound, "not_a_repository", err.Error())
	case strings.HasPrefix(err.Error(), "git "):
		writeProblem(w, http.StatusBadGateway, "git_failed", err.Error())
	default:
		writeProblem(w, http.StatusBadRequest, "invalid_git_request", err.Error())
	}
}
