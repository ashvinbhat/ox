package dashboard

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	"strings"
)

//go:embed templates/*.html
var content embed.FS

// Server represents the dashboard web server.
type Server struct {
	port      int
	oxHome    string
	yokePath  string
	oxPath    string
	templates *template.Template
}

// NewServer creates a new dashboard server.
func NewServer(port int, oxHome, yokePath, oxPath string) *Server {
	return &Server{
		port:     port,
		oxHome:   oxHome,
		yokePath: yokePath,
		oxPath:   oxPath,
	}
}

// Start starts the dashboard server.
func (s *Server) Start() error {
	// Parse templates
	var err error
	s.templates, err = template.ParseFS(content, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	// Routes
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/tasks", s.handleTasks)
	http.HandleFunc("/tasks/tree", s.handleTasksTree)
	http.HandleFunc("/tasks/ready", s.handleTasksReady)
	http.HandleFunc("/workspaces", s.handleWorkspaces)
	http.HandleFunc("/learnings", s.handleLearnings)

	// API routes
	http.HandleFunc("/api/tasks", s.apiTasks)
	http.HandleFunc("/api/tasks/tree", s.apiTasksTree)
	http.HandleFunc("/api/tasks/ready", s.apiTasksReady)
	http.HandleFunc("/api/tasks/summary", s.apiTasksSummary)
	http.HandleFunc("/api/workspaces", s.apiWorkspaces)
	http.HandleFunc("/api/workspaces/summary", s.apiWorkspacesSummary)
	http.HandleFunc("/api/worktrees", s.apiWorktrees)
	http.HandleFunc("/api/worktrees/summary", s.apiWorktreesSummary)
	http.HandleFunc("/api/learnings", s.apiLearnings)

	// Actions
	http.HandleFunc("/api/pickup", s.apiPickup)
	http.HandleFunc("/api/done", s.apiDone)
	http.HandleFunc("/api/open", s.apiOpen)
	http.HandleFunc("/api/start", s.apiStart)
	http.HandleFunc("/api/add", s.apiAddTask)

	addr := fmt.Sprintf(":%d", s.port)
	fmt.Printf("🐂 Ox Dashboard running at http://localhost%s\n", addr)
	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := map[string]interface{}{
		"Title":  "Home",
		"Active": "home",
	}
	s.templates.ExecuteTemplate(w, "index.html", data)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":  "Tasks",
		"Active": "tasks",
	}
	s.templates.ExecuteTemplate(w, "tasks.html", data)
}

func (s *Server) handleTasksTree(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":  "Task Tree",
		"Active": "tree",
	}
	s.templates.ExecuteTemplate(w, "tasks_tree.html", data)
}

func (s *Server) handleTasksReady(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":  "Ready Tasks",
		"Active": "ready",
	}
	s.templates.ExecuteTemplate(w, "tasks_ready.html", data)
}

func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":  "Workspaces",
		"Active": "workspaces",
	}
	s.templates.ExecuteTemplate(w, "workspaces.html", data)
}

func (s *Server) handleLearnings(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":  "Learnings",
		"Active": "learnings",
	}
	s.templates.ExecuteTemplate(w, "learnings.html", data)
}

// API handlers

func (s *Server) apiTasks(w http.ResponseWriter, r *http.Request) {
	output, err := s.runYoke("list")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tasks := s.parseTaskList(output)

	// Return HTML for HTMX
	w.Header().Set("Content-Type", "text/html")
	for _, task := range tasks {
		statusIcon := "○"
		switch task.Status {
		case "in_progress", "active":
			statusIcon = "●"
		case "done":
			statusIcon = "✓"
		case "blocked":
			statusIcon = "⊘"
		case "dropped":
			statusIcon = "✗"
		}

		tags := ""
		for _, tag := range task.Tags {
			tags += fmt.Sprintf(`<span class="bg-gray-700 text-gray-300 px-2 py-0.5 rounded text-xs mr-1">%s</span>`, tag)
		}

		fmt.Fprintf(w, `
		<div class="bg-gray-800 rounded-lg p-4 border border-gray-700 priority-%d hover:border-gray-500 transition">
			<div class="flex items-center justify-between">
				<div class="flex items-center space-x-3">
					<span class="status-%s text-lg">%s</span>
					<span class="text-gray-400 text-sm">#%d</span>
					<span class="font-medium">%s</span>
				</div>
				<div class="flex items-center space-x-2">
					%s
					<button hx-post="/api/start" hx-vals='{"task_id":"%s"}' hx-swap="none" class="bg-blue-600 hover:bg-blue-500 px-3 py-1 rounded text-sm">Start</button>
					<button hx-post="/api/open" hx-vals='{"task_id":"%s"}' hx-swap="none" class="bg-gray-600 hover:bg-gray-500 px-3 py-1 rounded text-sm">Open</button>
				</div>
			</div>
		</div>`, task.Priority, task.Status, statusIcon, task.Seq, task.Title, tags, task.ID, task.ID)
	}
}

func (s *Server) apiTasksTree(w http.ResponseWriter, r *http.Request) {
	output, err := s.runYoke("tree")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if strings.Contains(line, "task(s)") || line == "" {
			continue
		}

		// Calculate indent level by counting leading │ characters
		// Root: "├── ● #28" (0 leading │)
		// Child: "│   ├── ○ #27" (1 leading │)
		indentLevel := 0
		for _, char := range line {
			if char == '│' {
				indentLevel++
			} else if char != ' ' {
				break
			}
		}

		// Extract status icon
		isActive := strings.Contains(line, "●")
		isDone := strings.Contains(line, "✓")
		isBlocked := strings.Contains(line, "⊘")

		statusColor := "text-gray-400"
		statusIcon := "○"
		if isActive {
			statusColor = "text-blue-400"
			statusIcon = "●"
		} else if isDone {
			statusColor = "text-green-400"
			statusIcon = "✓"
		} else if isBlocked {
			statusColor = "text-red-400"
			statusIcon = "⊘"
		}

		// Extract task number
		seqStart := strings.Index(line, "#")
		if seqStart == -1 {
			continue
		}

		// Parse: #28 Title [tags]
		rest := line[seqStart:]
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) < 2 {
			continue
		}

		seq := strings.TrimPrefix(parts[0], "#")
		titleWithTags := parts[1]

		// Extract tags
		title := titleWithTags
		var tags []string
		if idx := strings.LastIndex(titleWithTags, "["); idx != -1 {
			if endIdx := strings.LastIndex(titleWithTags, "]"); endIdx > idx {
				tagStr := titleWithTags[idx+1 : endIdx]
				tags = strings.Split(tagStr, ", ")
				title = strings.TrimSpace(titleWithTags[:idx])
			}
		}

		// Build tag HTML
		tagHTML := ""
		for _, tag := range tags {
			tagHTML += fmt.Sprintf(`<span class="bg-gray-700 text-gray-300 px-2 py-0.5 rounded text-xs">%s</span> `, tag)
		}

		// Render as card with proper indentation
		marginLeft := indentLevel * 24
		borderClass := ""
		if indentLevel > 0 {
			borderClass = "border-l-2 border-gray-600"
		}

		fmt.Fprintf(w, `
		<div class="bg-gray-800 rounded-lg p-3 border border-gray-700 hover:border-gray-500 transition %s" style="margin-left: %dpx;">
			<div class="flex items-center justify-between">
				<div class="flex items-center space-x-3">
					<span class="%s text-lg">%s</span>
					<span class="text-gray-500 text-sm">#%s</span>
					<span class="font-medium text-gray-200">%s</span>
				</div>
				<div class="flex items-center space-x-1">
					%s
				</div>
			</div>
		</div>`, borderClass, marginLeft, statusColor, statusIcon, seq, title, tagHTML)
	}
}

func (s *Server) apiTasksReady(w http.ResponseWriter, r *http.Request) {
	output, err := s.runYoke("ready")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tasks := s.parseTaskList(output)

	// Return HTML for HTMX
	w.Header().Set("Content-Type", "text/html")
	if len(tasks) == 0 {
		fmt.Fprintf(w, `<div class="text-gray-400 text-center py-8">No tasks ready to work on</div>`)
		return
	}

	for _, task := range tasks {
		tags := ""
		for _, tag := range task.Tags {
			tags += fmt.Sprintf(`<span class="bg-gray-700 text-gray-300 px-2 py-0.5 rounded text-xs mr-1">%s</span>`, tag)
		}

		fmt.Fprintf(w, `
		<div class="bg-gray-800 rounded-lg p-4 border border-gray-700 priority-%d hover:border-green-500 transition cursor-pointer">
			<div class="flex items-center justify-between">
				<div class="flex items-center space-x-3">
					<span class="text-green-500 text-lg">🎯</span>
					<span class="text-gray-400 text-sm">#%d</span>
					<span class="font-medium">%s</span>
				</div>
				<div class="flex items-center space-x-2">
					%s
					<button hx-post="/api/start" hx-vals='{"task_id":"%s"}' hx-swap="none" class="bg-green-600 hover:bg-green-500 px-3 py-1 rounded text-sm">Start</button>
					<button hx-post="/api/open" hx-vals='{"task_id":"%s"}' hx-swap="none" class="bg-gray-600 hover:bg-gray-500 px-3 py-1 rounded text-sm">Open</button>
				</div>
			</div>
		</div>`, task.Priority, task.Seq, task.Title, tags, task.ID, task.ID)
	}
}

func (s *Server) apiWorkspaces(w http.ResponseWriter, r *http.Request) {
	output, err := s.runOx("status")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(output))
}

func (s *Server) apiWorktrees(w http.ResponseWriter, r *http.Request) {
	output, err := s.runOx("worktree list")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(output))
}

func (s *Server) apiLearnings(w http.ResponseWriter, r *http.Request) {
	output, err := s.runOx("learnings")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(output))
}

func (s *Server) apiTasksSummary(w http.ResponseWriter, r *http.Request) {
	output, err := s.runYoke("list")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tasks := s.parseTaskList(output)

	// Count by status
	pending, active, done := 0, 0, 0
	for _, t := range tasks {
		switch t.Status {
		case "pending":
			pending++
		case "in_progress", "active":
			active++
		case "done":
			done++
		}
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
		<div class="text-sm space-y-1">
			<div class="flex justify-between"><span class="text-gray-400">Total:</span><span>%d</span></div>
			<div class="flex justify-between"><span class="text-blue-400">Active:</span><span>%d</span></div>
			<div class="flex justify-between"><span class="text-gray-400">Pending:</span><span>%d</span></div>
			<div class="flex justify-between"><span class="text-green-400">Done:</span><span>%d</span></div>
		</div>
	`, len(tasks), active, pending, done)
}

func (s *Server) apiWorkspacesSummary(w http.ResponseWriter, r *http.Request) {
	output, err := s.runOx("status")
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="text-gray-400">No active workspaces</span>`)
		return
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}

	w.Header().Set("Content-Type", "text/html")
	if count == 0 {
		fmt.Fprintf(w, `<span class="text-gray-400">No active workspaces</span>`)
	} else {
		fmt.Fprintf(w, `<span class="text-blue-400">%d active workspace(s)</span>`, count)
	}
}

func (s *Server) apiWorktreesSummary(w http.ResponseWriter, r *http.Request) {
	output, err := s.runOx("worktree list")
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="text-gray-400">No worktrees</span>`)
		return
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}

	w.Header().Set("Content-Type", "text/html")
	if count == 0 {
		fmt.Fprintf(w, `<span class="text-gray-400">No worktrees</span>`)
	} else {
		fmt.Fprintf(w, `<span class="text-green-400">%d worktree(s)</span>`, count)
	}
}

func (s *Server) apiPickup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	repos := r.FormValue("repos")

	if taskID == "" || repos == "" {
		http.Error(w, "task_id and repos required", http.StatusBadRequest)
		return
	}

	output, err := s.runOx(fmt.Sprintf("pickup %s --repos %s", taskID, repos))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(output))
}

func (s *Server) apiDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	reason := r.FormValue("reason")

	cmd := fmt.Sprintf("done %s", taskID)
	if reason != "" {
		cmd += fmt.Sprintf(" --reason %q", reason)
	}

	output, err := s.runOx(cmd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(output))
}

func (s *Server) apiOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	output, err := s.runOx(fmt.Sprintf("open %s", taskID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(output))
}

func (s *Server) apiStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	output, err := s.runYoke(fmt.Sprintf("start %s", taskID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(output))
}

func (s *Server) apiAddTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	title := r.FormValue("title")
	tags := r.FormValue("tags")
	priority := r.FormValue("priority")
	parent := r.FormValue("parent")

	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	// Build args slice for proper argument handling
	var args []string
	if parent != "" {
		// Create as subtask
		args = append(args, "subtask", parent, title)
	} else {
		args = append(args, "add", title)
	}

	// Add tags
	if tags != "" {
		for _, tag := range strings.Split(tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				args = append(args, "-t", tag)
			}
		}
	}

	// Add priority
	if priority != "" && priority != "3" {
		args = append(args, "-p", priority)
	}

	cmd := exec.Command(s.yokePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("%s: %s", err, output), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="text-green-400 p-2">%s</div>`, strings.TrimSpace(string(output)))
}

// Helper functions

func (s *Server) runYoke(args string) (string, error) {
	cmd := exec.Command(s.yokePath, strings.Fields(args)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, output)
	}
	return string(output), nil
}

func (s *Server) runOx(args string) (string, error) {
	cmd := exec.Command(s.oxPath, strings.Fields(args)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, output)
	}
	return string(output), nil
}

type Task struct {
	Seq      int      `json:"seq"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags"`
}

func (s *Server) parseTaskList(output string) []Task {
	var tasks []Task
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") || strings.Contains(line, "task(s)") || line == "" {
			continue
		}

		// Parse: SEQ   ID     STATUS       PRI  TITLE [tags]
		// Note: STATUS can be "IN PROGRESS" (two words) or "pending" (one word)
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		var seq int
		fmt.Sscanf(fields[0], "%d", &seq)

		id := fields[1]

		// Detect multi-word status by checking if priority field is where expected
		var status string
		var priority int
		var titleStartIdx int

		// Check if fields[3] looks like a priority (P1, P2, etc.)
		if len(fields) > 3 && len(fields[3]) == 2 && fields[3][0] == 'P' && fields[3][1] >= '1' && fields[3][1] <= '9' {
			// Single word status
			status = strings.ToLower(fields[2])
			fmt.Sscanf(fields[3], "P%d", &priority)
			titleStartIdx = 4
		} else if len(fields) > 4 && len(fields[4]) == 2 && fields[4][0] == 'P' && fields[4][1] >= '1' && fields[4][1] <= '9' {
			// Two word status (e.g., "IN PROGRESS")
			status = strings.ToLower(fields[2] + "_" + fields[3])
			fmt.Sscanf(fields[4], "P%d", &priority)
			titleStartIdx = 5
		} else {
			// Fallback - assume single word status
			status = strings.ToLower(fields[2])
			if len(fields) > 3 {
				fmt.Sscanf(fields[3], "P%d", &priority)
			}
			titleStartIdx = 4
		}

		// Rest is title and tags
		if titleStartIdx >= len(fields) {
			continue
		}
		titleParts := fields[titleStartIdx:]
		title := strings.Join(titleParts, " ")

		// Extract tags from end
		var tags []string
		if idx := strings.LastIndex(title, "["); idx != -1 {
			if endIdx := strings.LastIndex(title, "]"); endIdx > idx {
				tagStr := title[idx+1 : endIdx]
				tags = strings.Split(tagStr, ", ")
				title = strings.TrimSpace(title[:idx])
			}
		}

		tasks = append(tasks, Task{
			Seq:      seq,
			ID:       id,
			Title:    title,
			Status:   status,
			Priority: priority,
			Tags:     tags,
		})
	}

	return tasks
}
