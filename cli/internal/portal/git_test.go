package portal

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRepo makes a repository with one commit on main and a remote
// clone to push to.
func newTestRepo(t *testing.T) (dir string, remote string) {
	t.Helper()
	dir, remote = t.TempDir(), t.TempDir()
	run := func(d string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@example.com", "GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@example.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run(remote, "init", "--bare", "-q", "-b", "main")
	run(dir, "init", "-q", "-b", "main")
	run(dir, "config", "user.name", "Ada")
	run(dir, "config", "user.email", "ada@example.com")
	run(dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(dir, "add", ".")
	run(dir, "commit", "-q", "-m", "Initial")
	run(dir, "remote", "add", "origin", remote)
	run(dir, "push", "-q", "-u", "origin", "main")
	return dir, remote
}

func TestGitWorkflow(t *testing.T) {
	dir, _ := newTestRepo(t)
	g := NewGit(dir)
	ctx := context.Background()
	if !g.Available(ctx) || NewGit(t.TempDir()).Available(ctx) {
		t.Fatal("Available")
	}
	st, err := g.Status(ctx)
	if err != nil || st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 0 || len(st.Files) != 0 || st.Remote != "origin" {
		t.Fatalf("clean status = %+v, %v", st, err)
	}

	// Changes: modified, untracked; diffs; stage; unstage; discard.
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# app\n\nMore.\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600)
	st, _ = g.Status(ctx)
	if st.Unstaged != 1 || st.Untracked != 1 || len(st.Files) != 2 {
		t.Errorf("dirty status = %+v", st)
	}
	d, err := g.Diff(ctx, "README.md", false)
	if err != nil || d.Additions != 2 || d.Deletions != 0 || !strings.Contains(d.Patch, "+More.") {
		t.Errorf("diff = %+v, %v", d, err)
	}
	if d, err := g.Diff(ctx, "new.txt", false); err != nil || !d.Untracked || d.Additions != 1 {
		t.Errorf("untracked diff = %+v, %v", d, err)
	}
	if _, err := g.Diff(ctx, "../etc/passwd", false); err == nil {
		t.Error("a path outside the repository was accepted")
	}
	if err := g.Stage(ctx, []string{"README.md", "new.txt"}); err != nil {
		t.Fatal(err)
	}
	st, _ = g.Status(ctx)
	if st.Staged != 2 || st.Unstaged != 0 || st.Untracked != 0 {
		t.Errorf("staged status = %+v", st)
	}
	if d, _ := g.Diff(ctx, "README.md", true); d.Additions != 2 {
		t.Errorf("staged diff = %+v", d)
	}
	if err := g.Unstage(ctx, []string{"new.txt"}); err != nil {
		t.Fatal(err)
	}
	st, _ = g.Status(ctx)
	if st.Staged != 1 || st.Untracked != 1 {
		t.Errorf("after unstage = %+v", st)
	}
	// A hunk staged from a patch: unstage README, then apply its diff.
	_ = g.Unstage(ctx, []string{"README.md"})
	d, _ = g.Diff(ctx, "README.md", false)
	if err := g.ApplyPatch(ctx, d.Patch, false); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if st, _ := g.Status(ctx); st.Staged != 1 {
		t.Errorf("after patch = %+v", st)
	}
	if err := g.ApplyPatch(ctx, d.Patch, true); err != nil {
		t.Fatalf("ApplyPatch reverse: %v", err)
	}
	if err := g.Discard(ctx, []string{"README.md", "new.txt"}); err != nil {
		t.Fatal(err)
	}
	if st, _ := g.Status(ctx); len(st.Files) != 0 {
		t.Errorf("after discard = %+v", st)
	}

	// Commit on a branch, log, merge preview and merge, push.
	if err := g.CreateBranch(ctx, "feature/x", "", true); err != nil {
		t.Fatal(err)
	}
	if err := g.CreateBranch(ctx, "bad name", "", false); err == nil {
		t.Error("a bad branch name was accepted")
	}
	_ = os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("f\n"), 0o600)
	_ = g.Stage(ctx, []string{"feature.txt"})
	if _, err := g.Commit(ctx, "  "); err == nil {
		t.Error("an empty message was accepted")
	}
	c, err := g.Commit(ctx, "Add feature\n\nBody here.")
	if err != nil || c.Subject != "Add feature" || c.Body != "Body here." || c.Author != "Ada" || len(c.Parents) != 1 {
		t.Fatalf("commit = %+v, %v", c, err)
	}
	branches, remotes, err := g.Branches(ctx)
	if err != nil || len(branches) != 2 || len(remotes) != 1 || remotes[0] != "origin/main" {
		t.Fatalf("branches = %+v %v, %v", branches, remotes, err)
	}
	for _, b := range branches {
		if b.Name == "feature/x" && (!b.Current || b.Merged != true) {
			t.Errorf("feature branch = %+v", b)
		}
	}
	if err := g.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	unmerged, _ := g.UnmergedCommits(ctx, "feature/x")
	if len(unmerged) != 1 || unmerged[0].Subject != "Add feature" {
		t.Errorf("unmerged = %+v", unmerged)
	}
	p, err := g.MergePreview(ctx, "feature/x")
	if err != nil || !p.FastForward || len(p.Commits) != 1 || len(p.Conflicts) != 0 || strings.Join(p.Files, ",") != "feature.txt" {
		t.Errorf("preview = %+v, %v", p, err)
	}
	if res, err := g.Merge(ctx, "feature/x"); err != nil || len(res.Conflicts) != 0 {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	if p, _ := g.MergePreview(ctx, "feature/x"); !p.UpToDate {
		t.Errorf("preview after merge = %+v", p)
	}
	if err := g.DeleteBranch(ctx, "feature/x", false); err != nil {
		t.Errorf("delete merged branch: %v", err)
	}
	st, _ = g.Status(ctx)
	if st.Ahead != 1 {
		t.Errorf("ahead = %+v", st)
	}
	if res, err := g.Push(ctx); err != nil {
		t.Fatalf("push = %+v, %v", res, err)
	}
	if st, _ := g.Status(ctx); st.Ahead != 0 {
		t.Errorf("after push = %+v", st)
	}
	if _, err := g.Fetch(ctx); err != nil {
		t.Errorf("fetch: %v", err)
	}
	if res, err := g.Pull(ctx); err != nil || len(res.Conflicts) != 0 {
		t.Errorf("pull = %+v, %v", res, err)
	}
	log, err := g.Log(ctx, GitLogOptions{All: true, Limit: 10})
	if err != nil || len(log) != 2 || log[0].Subject != "Add feature" || len(log[0].Refs) == 0 {
		t.Errorf("log = %+v, %v", log, err)
	}
	if _, err := g.Log(ctx, GitLogOptions{Range: "--output=/tmp/x"}); err == nil {
		t.Error("an option as a range was accepted")
	}

	// A conflicting merge is previewed, left in the tree, then aborted.
	_ = g.CreateBranch(ctx, "conflict", "", true)
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# theirs\n"), 0o600)
	_ = g.Stage(ctx, []string{"README.md"})
	_, _ = g.Commit(ctx, "Theirs")
	_ = g.Switch(ctx, "main")
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# ours\n"), 0o600)
	_ = g.Stage(ctx, []string{"README.md"})
	_, _ = g.Commit(ctx, "Ours")
	p, err = g.MergePreview(ctx, "conflict")
	if err != nil || p.FastForward || strings.Join(p.Conflicts, ",") != "README.md" {
		t.Errorf("conflict preview = %+v, %v", p, err)
	}
	res, err := g.Merge(ctx, "conflict")
	if err != nil || strings.Join(res.Conflicts, ",") != "README.md" {
		t.Fatalf("conflicting merge = %+v, %v", res, err)
	}
	if st, _ := g.Status(ctx); st.State != "merging" || st.Conflicts != 1 {
		t.Errorf("merging status = %+v", st)
	}
	if err := g.AbortMerge(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := g.Status(ctx); st.State != "" || len(st.Files) != 0 {
		t.Errorf("after abort = %+v", st)
	}
	if err := g.DeleteBranch(ctx, "conflict", false); err == nil {
		t.Error("an unmerged branch was deleted without force")
	}
	if err := g.DeleteBranch(ctx, "conflict", true); err != nil {
		t.Errorf("force delete: %v", err)
	}
}

func TestGitEndpoints(t *testing.T) {
	dir, _ := newTestRepo(t)
	opened := ""
	_, ts, _ := newTestServer(t, func(c *Config) {
		c.Git = NewGit(dir)
		c.OpenInEditor = func(path string, line int) error { opened = path; return nil }
	})
	get := func(path string) (int, string) {
		res := call(t, ts, http.MethodGet, APIPrefix+path, "", nil)
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	post := func(path, body string) (int, string) {
		res := call(t, ts, http.MethodPost, APIPrefix+path, body, nil)
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	if code, body := get("git/status"); code != http.StatusOK || !strings.Contains(body, `"branch":"main"`) {
		t.Fatalf("status = %d %s", code, body)
	}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o600)
	if code, body := get("git/diff?path=a.txt"); code != http.StatusOK || !strings.Contains(body, `"untracked":true`) {
		t.Errorf("diff = %d %s", code, body)
	}
	if code, body := post("git/stage", `{"paths":["a.txt"]}`); code != http.StatusOK || !strings.Contains(body, `"staged":1`) {
		t.Errorf("stage = %d %s", code, body)
	}
	if code, body := post("git/commit", `{"message":"Add a"}`); code != http.StatusOK || !strings.Contains(body, `"subject":"Add a"`) {
		t.Errorf("commit = %d %s", code, body)
	}
	if code, body := post("git/commit", `{"message":"Nothing"}`); code != http.StatusConflict || !strings.Contains(body, "git_refused") {
		t.Errorf("empty commit = %d %s", code, body)
	}
	if code, body := post("git/branches", `{"name":"topic","checkout":true}`); code != http.StatusOK || !strings.Contains(body, `"name":"topic","current":true`) {
		t.Errorf("create branch = %d %s", code, body)
	}
	if code, body := post("git/switch", `{"name":"main"}`); code != http.StatusOK || !strings.Contains(body, `"branch":"main"`) {
		t.Errorf("switch = %d %s", code, body)
	}
	if code, body := get("git/unmerged?branch=topic"); code != http.StatusOK || !strings.Contains(body, `"commits":[]`) {
		t.Errorf("unmerged = %d %s", code, body)
	}
	if res := call(t, ts, http.MethodDelete, APIPrefix+"git/branches/topic", "", nil); res.StatusCode != http.StatusOK {
		t.Errorf("delete branch = %d", res.StatusCode)
	}
	if code, body := get("git/merge/preview?branch=main"); code != http.StatusOK || !strings.Contains(body, `"up_to_date":true`) {
		t.Errorf("preview = %d %s", code, body)
	}
	if code, body := post("git/push", ``); code != http.StatusOK || !strings.Contains(body, `"output"`) {
		t.Errorf("push = %d %s", code, body)
	}
	if code, body := get("git/log?limit=5&all=true"); code != http.StatusOK || !strings.Contains(body, `"subject":"Add a"`) {
		t.Errorf("log = %d %s", code, body)
	}
	if code, _ := post("git/open", `{"path":"a.txt","line":1}`); code != http.StatusNoContent || opened != "a.txt" {
		t.Errorf("open = %d, opened %q", code, opened)
	}
	if code, body := post("git/stage", `{"paths":["../x"]}`); code != http.StatusBadRequest || !strings.Contains(body, "invalid_git_request") {
		t.Errorf("bad path = %d %s", code, body)
	}
	_, ts2, _ := newTestServer(t, func(c *Config) { c.Git = NewGit(t.TempDir()) })
	if res := call(t, ts2, http.MethodGet, APIPrefix+"git/status", "", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("not a repository = %d", res.StatusCode)
	}
}
