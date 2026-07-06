// Package cockpit is the live mission dashboard: one page served from the ox
// binary showing every mission, streaming any agent's terminal in real time
// (tmux control mode → SSE), with an input bar that types into the selected
// agent. It replaces the old yoke-text-scraping dashboard.
package cockpit

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/job"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

//go:embed assets/*
var assets embed.FS

type Server struct {
	cfg  *config.Config
	port int
}

func New(cfg *config.Config, port int) *Server {
	return &Server{cfg: cfg, port: port}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	static, _ := fs.Sub(assets, "assets")
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/api/send", s.handleSend)

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	fmt.Printf("ox cockpit: http://%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

// ---- state ----

type sessionRef struct {
	Label  string `json:"label"`
	Target string `json:"target"`
	Live   bool   `json:"live"`
	Status string `json:"status,omitempty"`
	Model  string `json:"model,omitempty"`
	Spend  string `json:"spend,omitempty"`
}

type jobRef struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Model  string `json:"model"`
	Cost   string `json:"cost"`
}

type missionState struct {
	ID       string       `json:"id"`
	Goal     string       `json:"goal"`
	Type     string       `json:"type"`
	Phase    string       `json:"phase"`
	TaskSeq  int          `json:"task_seq,omitempty"`
	Spend    string       `json:"spend"`
	Budget   string       `json:"budget"`
	Frozen   bool         `json:"frozen,omitempty"`
	Sessions []sessionRef `json:"sessions"`
	Jobs     []jobRef     `json:"jobs,omitempty"`
	Events   []string     `json:"events,omitempty"`
	PRs      []string     `json:"prs,omitempty"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	missions, err := mission.List(s.cfg.Home)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var out []missionState
	for _, m := range missions {
		if !m.Open() {
			continue
		}
		ms := missionState{
			ID: m.ID, Goal: m.Goal, Type: m.Type, Phase: m.Phase,
			Spend:  fmt.Sprintf("$%.2f", m.SpentUSD),
			Budget: fmt.Sprintf("$%.2f", m.Budgets.MissionUSD),
			Frozen: m.SpendFrozen,
		}
		if m.Yoke != nil {
			ms.TaskSeq = m.Yoke.Seq
		}

		session := m.TmuxSession()
		ms.Sessions = append(ms.Sessions,
			sessionRef{Label: "orchestrator", Target: session + ":orc", Live: tmuxutil.HasWindow(session, "orc")},
			sessionRef{Label: "watcher", Target: session + ":watch", Live: tmuxutil.HasWindow(session, "watch")},
		)

		if reg, err := harness.LoadRegistry(m); err == nil {
			for _, wk := range sortedWorkers(reg) {
				ms.Sessions = append(ms.Sessions, sessionRef{
					Label: wk.ID, Target: wk.TmuxSession,
					Live:   tmuxutil.HasSession(wk.TmuxSession),
					Status: wk.Status, Model: wk.Model,
					Spend: fmt.Sprintf("$%.2f", wk.SpendUSD),
				})
			}
		}

		if idx, err := job.LoadIndex(m); err == nil {
			for _, j := range idx.Jobs {
				ms.Jobs = append(ms.Jobs, jobRef{
					ID: j.ID, Status: j.Status, Model: j.Model,
					Cost: fmt.Sprintf("$%.3f", j.CostUSD),
				})
			}
		}

		if events, err := m.EventsSince(0); err == nil {
			start := max(0, len(events)-6)
			for _, ev := range events[start:] {
				ms.Events = append(ms.Events, fmt.Sprintf("%s %s", ev.TS.Format("15:04"), ev.Type))
			}
		}
		for _, pr := range m.PRs {
			ms.PRs = append(ms.PRs, pr.URL)
		}
		out = append(out, ms)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"missions": out, "now": time.Now().Format("15:04:05")})
}

func sortedWorkers(reg *harness.Registry) []*harness.Worker {
	var ids []string
	for id := range reg.Workers {
		ids = append(ids, id)
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	out := make([]*harness.Worker, 0, len(ids))
	for _, id := range ids {
		out = append(out, reg.Workers[id])
	}
	return out
}

// ---- terminal stream (SSE) ----

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" || !strings.HasPrefix(target, "ox-") {
		http.Error(w, "target must be an ox tmux target", 400)
		return
	}
	session := target
	if i := strings.IndexByte(target, ':'); i > 0 {
		session = target[:i]
	}
	if !tmuxutil.HasSession(session) {
		http.Error(w, "session not running", 404)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	paneID, err := tmuxutil.PaneID(target)
	if err != nil {
		sse(w, flusher, "err", err.Error())
		return
	}
	cols, rows := tmuxutil.PaneSize(target)
	meta, _ := json.Marshal(map[string]int{"cols": cols, "rows": rows})
	sse(w, flusher, "meta", string(meta))

	// Attach BEFORE snapshotting so no output falls between the two; deltas
	// that arrive while we capture are already part of the snapshot and get
	// drained below.
	cs, err := tmuxutil.OpenControlStream(session)
	if err != nil {
		sse(w, flusher, "err", "control mode attach failed: "+err.Error())
		return
	}
	defer cs.Close()

	snap, err := tmuxutil.CaptureVisible(target)
	if err != nil {
		sse(w, flusher, "err", err.Error())
		return
	}
	curX, curY := tmuxutil.CursorPos(target)
drain:
	for {
		select {
		case <-cs.Events:
		default:
			break drain
		}
	}
	// Clear, paint the current screen, then park the cursor where tmux has
	// it so absolute-addressed deltas line up. capture-pane separates rows
	// with bare LF, which a terminal renders as move-down-keep-column — the
	// staircase effect — so rows are joined with CRLF.
	screen := strings.ReplaceAll(strings.TrimRight(snap, "\n"), "\n", "\r\n")
	frame := "\x1b[2J\x1b[H" + screen + fmt.Sprintf("\x1b[%d;%dH", curY+1, curX+1)
	sse(w, flusher, "full", encodeChunk(frame))

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			sse(w, flusher, "ping", "1")
		case ev, ok := <-cs.Events:
			if !ok || ev.Exit {
				sse(w, flusher, "err", "session ended")
				return
			}
			if ev.PaneID != paneID {
				continue
			}
			sse(w, flusher, "out", encodeChunk(ev.Data))
		}
	}
}

// encodeChunk base64s terminal bytes: SSE frames are newline-delimited and
// terminal output is full of newlines and control bytes.
func encodeChunk(s string) string {
	return base64Encode([]byte(s))
}

func sse(w http.ResponseWriter, f http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	f.Flush()
}

// handleHistory serves the pane's scrollback as one colored transcript for
// the read-only history view.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" || !strings.HasPrefix(target, "ox-") {
		http.Error(w, "target must be an ox tmux target", 400)
		return
	}
	hist, err := tmuxutil.CaptureHistory(target, 8000)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	cols, _ := tmuxutil.PaneSize(target)
	screen := strings.ReplaceAll(strings.TrimRight(hist, "\n"), "\n", "\r\n")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"cols": cols, "data": encodeChunk(screen)})
}

// ---- input ----

type sendReq struct {
	Target     string `json:"target"`
	Text       string `json:"text"`
	Background bool   `json:"background,omitempty"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var req sendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Target == "" || !strings.HasPrefix(req.Target, "ox-") || strings.TrimSpace(req.Text) == "" {
		http.Error(w, "ox target and text required", 400)
		return
	}

	text := req.Text
	if req.Background {
		text = "/btw " + text
	}
	if err := harness.SendMessageEnsured(req.Target, text); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
