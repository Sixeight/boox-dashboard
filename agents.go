package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Agent is a running Claude Code session, as recorded in <config dir>/sessions/<pid>.json.
type Agent struct {
	Name       string `json:"name"`
	Project    string `json:"project"`
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
	SinceSec   int64  `json:"sinceSec"`
	Title      string `json:"title"`
	LastPrompt string `json:"lastPrompt"`
	Tool       string `json:"tool"`
	Attention  string `json:"attention"`
}

// Attention values: what the user is expected to do for a session.
const (
	attentionWaiting = "waiting" // a permission prompt or dialog is open
	attentionReply   = "reply"   // the turn just finished and the session awaits the next message
)

// replyWindow is how long an idle session counts as awaiting a reply; older idle sessions are treated as left alone.
const replyWindow = 30 * time.Minute

func attention(status string, since time.Duration) string {
	switch {
	case status == "waiting":
		return attentionWaiting
	case status == "idle" && since < replyWindow:
		return attentionReply
	default:
		return ""
	}
}

// attentionRank orders sessions: waiting, then reply, then busy, then the rest.
func attentionRank(a Agent) int {
	switch {
	case a.Attention == attentionWaiting:
		return 0
	case a.Attention == attentionReply:
		return 1
	case a.Status == "busy":
		return 2
	default:
		return 3
	}
}

// transcriptTailBytes bounds how much of each transcript is read; titles and prompts are re-recorded often enough to appear in it.
const transcriptTailBytes = 256 << 10

func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// listAgents returns live sessions ordered by attentionRank, then most recently changed first.
// A session file is live only if its pid still runs with the recorded start time,
// since session files can outlive their process and pids get reused.
func listAgents(sessionsDir, projectsDir, procDir string, now time.Time) ([]Agent, error) {
	paths, err := filepath.Glob(filepath.Join(sessionsDir, "*.json"))
	if err != nil {
		return nil, err
	}

	var agents []Agent
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		var s struct {
			PID             int    `json:"pid"`
			SessionID       string `json:"sessionId"`
			ProcStart       string `json:"procStart"`
			Cwd             string `json:"cwd"`
			Name            string `json:"name"`
			Status          string `json:"status"`
			WaitingFor      string `json:"waitingFor"`
			StatusUpdatedAt int64  `json:"statusUpdatedAt"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		if procStartTime(procDir, s.PID) != s.ProcStart {
			continue
		}
		since := now.Sub(time.UnixMilli(s.StatusUpdatedAt))
		a := Agent{
			Name:       s.Name,
			Project:    filepath.Base(s.Cwd),
			Status:     s.Status,
			WaitingFor: s.WaitingFor,
			SinceSec:   since.Milliseconds() / 1000,
			Attention:  attention(s.Status, since),
		}
		// Only sessions that need attention are listed on the page, so only they need their transcript.
		if a.Attention != "" {
			info := readTranscriptInfo(projectsDir, s.SessionID)
			a.Title, a.LastPrompt, a.Tool = info.Title, info.LastPrompt, info.Tool
		}
		agents = append(agents, a)
	}

	sort.SliceStable(agents, func(i, j int) bool {
		ri, rj := attentionRank(agents[i]), attentionRank(agents[j])
		if ri != rj {
			return ri < rj
		}
		return agents[i].SinceSec < agents[j].SinceSec
	})
	return agents, nil
}

type transcriptInfo struct {
	Title      string
	LastPrompt string
	Tool       string // a tool_use still waiting for its result
}

// readTranscriptInfo reads the tail of <projects>/<any project>/<sessionID>.jsonl.
func readTranscriptInfo(projectsDir, sessionID string) transcriptInfo {
	if sessionID == "" {
		return transcriptInfo{}
	}
	paths, _ := filepath.Glob(filepath.Join(projectsDir, "*", sessionID+".jsonl"))
	if len(paths) == 0 {
		return transcriptInfo{}
	}
	f, err := os.Open(paths[0])
	if err != nil {
		return transcriptInfo{}
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > transcriptTailBytes {
		f.Seek(-transcriptTailBytes, io.SeekEnd)
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return transcriptInfo{}
	}
	return parseTranscriptTail(tail)
}

// parseTranscriptTail scans transcript lines in order; later records win. Unparsable lines,
// such as the first line cut by seeking into the file, are skipped.
func parseTranscriptTail(tail []byte) transcriptInfo {
	var info transcriptInfo
	var pendingID string // the latest tool_use; info.Tool holds its name until its result arrives
	for _, line := range bytes.Split(tail, []byte("\n")) {
		var rec struct {
			Type       string `json:"type"`
			AITitle    string `json:"aiTitle"`
			LastPrompt string `json:"lastPrompt"`
			Message    struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		switch rec.Type {
		case "ai-title":
			info.Title = rec.AITitle
		case "last-prompt":
			info.LastPrompt = rec.LastPrompt
		case "assistant", "user":
			var blocks []struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				ToolUseID string `json:"tool_use_id"`
			}
			if json.Unmarshal(rec.Message.Content, &blocks) != nil {
				continue // string content
			}
			for _, b := range blocks {
				switch b.Type {
				case "tool_use":
					pendingID, info.Tool = b.ID, b.Name
				case "tool_result":
					if b.ToolUseID == pendingID {
						info.Tool = ""
					}
				}
			}
		}
	}
	return info
}

// procStartTime returns field 22 (starttime) of /proc/<pid>/stat, or "" if the process is gone.
func procStartTime(procDir string, pid int) string {
	raw, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "stat"))
	if err != nil {
		return ""
	}
	// comm (field 2) may contain spaces and parentheses; fields after the last ')' start at field 3.
	i := strings.LastIndex(string(raw), ") ")
	if i < 0 {
		return ""
	}
	fields := strings.Fields(string(raw)[i+2:])
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}
