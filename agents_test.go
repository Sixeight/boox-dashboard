package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// procStat builds a /proc/<pid>/stat line whose 22nd field (starttime) is start.
// The comm field contains a space and a ')' to exercise parsing after the last ')'.
func procStat(pid, start string) string {
	return pid + " (claude (x) y) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 " + start + " 20 21\n"
}

func TestListAgents(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	projects := filepath.Join(root, "projects")
	proc := filepath.Join(root, "proc")

	// Live and busy. Its transcript is not read: the page lists only sessions that need attention.
	writeFile(t, filepath.Join(sessions, "100.json"),
		`{"pid":100,"sessionId":"s-100","procStart":"5000","cwd":"/src/github.com/sixeight/boox-dashboard","name":"boox-dashboard-dc","kind":"interactive","status":"busy","statusUpdatedAt":1792821800000}`)
	writeFile(t, filepath.Join(proc, "100", "stat"), procStat("100", "5000"))
	writeFile(t, filepath.Join(projects, "-src-github-com-sixeight-boox-dashboard", "s-100.jsonl"),
		`{"type":"ai-title","aiTitle":"Booxのダッシュボード化"}`+"\n")
	// Live and idle, updated more recently than the busy one, with a transcript.
	writeFile(t, filepath.Join(sessions, "200.json"),
		`{"pid":200,"sessionId":"s-200","procStart":"6000","cwd":"/src/github.com/sixeight/rui","name":"rui-ce","kind":"bg","status":"idle","statusUpdatedAt":1792821850000}`)
	writeFile(t, filepath.Join(proc, "200", "stat"), procStat("200", "6000"))
	writeFile(t, filepath.Join(projects, "-src-github-com-sixeight-rui", "s-200.jsonl"),
		`{"type":"ai-title","aiTitle":"rui のリファクタ"}`+"\n"+`{"type":"last-prompt","lastPrompt":"試しに出してみて"}`+"\n")
	// PID reused by another process: start time differs.
	writeFile(t, filepath.Join(sessions, "300.json"),
		`{"pid":300,"procStart":"7000","cwd":"/x","name":"stale","kind":"interactive","status":"busy","statusUpdatedAt":1792821860000}`)
	writeFile(t, filepath.Join(proc, "300", "stat"), procStat("300", "9999"))
	// Process gone.
	writeFile(t, filepath.Join(sessions, "400.json"),
		`{"pid":400,"procStart":"8000","cwd":"/y","name":"gone","kind":"interactive","status":"idle","statusUpdatedAt":1792821870000}`)
	// Waiting on a permission prompt for an hour: still needs attention.
	writeFile(t, filepath.Join(sessions, "500.json"),
		`{"pid":500,"procStart":"500","cwd":"/src/waku","name":"waku-79","kind":"interactive","status":"waiting","waitingFor":"permission prompt","statusUpdatedAt":1792818300000}`)
	writeFile(t, filepath.Join(proc, "500", "stat"), procStat("500", "500"))
	// Idle for two hours: left alone, not a pending reply.
	writeFile(t, filepath.Join(sessions, "600.json"),
		`{"pid":600,"procStart":"600","cwd":"/src/old","name":"old","kind":"interactive","status":"idle","statusUpdatedAt":1792814700000}`)
	writeFile(t, filepath.Join(proc, "600", "stat"), procStat("600", "600"))
	// Not a session file.
	writeFile(t, filepath.Join(sessions, "100.abc.key"), "secret")

	now := time.UnixMilli(1792821900000)
	got, err := listAgents(sessions, projects, proc, now)
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{
		{Name: "waku-79", Project: "waku", Status: "waiting", WaitingFor: "permission prompt", SinceSec: 3600,
			Attention: attentionWaiting},
		{Name: "rui-ce", Project: "rui", Status: "idle", SinceSec: 50, Attention: attentionReply,
			Title: "rui のリファクタ", LastPrompt: "試しに出してみて"},
		{Name: "boox-dashboard-dc", Project: "boox-dashboard", Status: "busy", SinceSec: 100},
		{Name: "old", Project: "old", Status: "idle", SinceSec: 7200},
	}
	if len(got) != len(want) {
		t.Fatalf("listAgents = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("listAgents[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseTranscriptTail(t *testing.T) {
	// The tail starts mid-line, as it does after seeking into a large file.
	tail := `ial line cut by the seek"}
{"type":"ai-title","aiTitle":"古いタイトル"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}}
{"type":"last-prompt","lastPrompt":"試しに出してみて"}
{"type":"ai-title","aiTitle":"Booxのダッシュボード化"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"調べる"},{"type":"tool_use","id":"t2","name":"Bash"}]}}
`
	got := parseTranscriptTail([]byte(tail))
	want := transcriptInfo{Title: "Booxのダッシュボード化", LastPrompt: "試しに出してみて", Tool: "Bash"}
	if got != want {
		t.Errorf("parseTranscriptTail = %+v, want %+v", got, want)
	}
}

func TestParseTranscriptTailNoPendingTool(t *testing.T) {
	tail := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}}
{"type":"user","message":{"role":"user","content":"plain string content"}}
`
	if got := parseTranscriptTail([]byte(tail)); got.Tool != "" {
		t.Errorf("Tool = %q, want empty", got.Tool)
	}
}

func TestListAgentsMissingDir(t *testing.T) {
	dir := t.TempDir()
	got, err := listAgents(filepath.Join(dir, "nope"), filepath.Join(dir, "projects"), "/proc", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("listAgents = %+v, want empty", got)
	}
}
