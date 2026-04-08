package dashboard

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/feedback"
)

//go:embed templates/*.html
var content embed.FS

// Server represents the dashboard web server.
type Server struct {
	port          int
	oxHome        string
	yokePath      string
	oxPath        string
	templates     *template.Template
	feedbackStore *feedback.Store
	config        *config.Config
	sessions      map[string]bool
	sessionMu     sync.RWMutex
}

// NewServer creates a new dashboard server.
func NewServer(port int, oxHome, yokePath, oxPath string) *Server {
	fs := feedback.NewStore(oxHome)
	fs.Init() // Initialize feedback storage

	cfg, _ := config.Load()

	return &Server{
		port:          port,
		oxHome:        oxHome,
		yokePath:      yokePath,
		oxPath:        oxPath,
		feedbackStore: fs,
		config:        cfg,
		sessions:      make(map[string]bool),
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
	http.HandleFunc("/task/", s.handleTaskDetail)
	http.HandleFunc("/workspaces", s.handleWorkspaces)
	http.HandleFunc("/learnings", s.handleLearnings)

	// API routes
	http.HandleFunc("/api/tasks", s.apiTasks)
	http.HandleFunc("/api/tasks/done", s.apiTasksDone)
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
	http.HandleFunc("/api/block", s.apiBlock)
	http.HandleFunc("/api/drop", s.apiDrop)
	http.HandleFunc("/api/open", s.apiOpen)
	http.HandleFunc("/api/start", s.apiStart)
	http.HandleFunc("/api/add", s.apiAddTask)
	http.HandleFunc("/api/review", s.apiReview)
	http.HandleFunc("/api/reopen", s.apiReopen)

	// Feedback routes (password protected)
	http.HandleFunc("/feedback", s.feedbackAuth(s.handleFeedback))
	http.HandleFunc("/feedback/week", s.feedbackAuth(s.handleFeedbackWeek))
	http.HandleFunc("/feedback/prepare", s.feedbackAuth(s.handleFeedbackPrepare))
	http.HandleFunc("/feedback/person/", s.feedbackAuth(s.handleFeedbackPerson))
	http.HandleFunc("/feedback/login", s.handleFeedbackLogin)

	// Feedback API (password protected)
	http.HandleFunc("/api/feedback/people", s.feedbackAuth(s.apiFeedbackPeople))
	http.HandleFunc("/api/feedback/people/search", s.feedbackAuth(s.apiFeedbackPeopleSearch))
	http.HandleFunc("/api/feedback/people/add", s.feedbackAuth(s.apiFeedbackPeopleAdd))
	http.HandleFunc("/api/feedback/observations", s.feedbackAuth(s.apiFeedbackObservations))
	http.HandleFunc("/api/feedback/observation/add", s.feedbackAuth(s.apiFeedbackObservationAdd))
	http.HandleFunc("/api/feedback/observation/edit", s.feedbackAuth(s.apiFeedbackObservationEdit))
	http.HandleFunc("/api/feedback/observation/delete", s.feedbackAuth(s.apiFeedbackObservationDelete))
	http.HandleFunc("/api/feedback/week/complete", s.feedbackAuth(s.apiFeedbackWeekComplete))
	http.HandleFunc("/api/feedback/summaries", s.feedbackAuth(s.apiFeedbackSummaries))
	http.HandleFunc("/api/feedback/cycles", s.feedbackAuth(s.apiFeedbackCycles))
	http.HandleFunc("/api/feedback/tasks", s.feedbackAuth(s.apiFeedbackTasks))

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

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/task/")
	if taskID == "" {
		http.NotFound(w, r)
		return
	}

	// Get task details
	taskOutput, err := s.runYoke(fmt.Sprintf("show %s", taskID))
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// Get task notes
	notesOutput, _ := s.runYoke(fmt.Sprintf("notes %s", taskID))

	// Get task log/history
	logOutput, _ := s.runYoke(fmt.Sprintf("log %s", taskID))

	// Get checkpoints (from ox) - need to run from workspace directory
	checkpointsOutput := s.getCheckpointsForTask(taskID)

	data := map[string]interface{}{
		"Title":       fmt.Sprintf("Task #%s", taskID),
		"Active":      "tasks",
		"TaskID":      taskID,
		"TaskDetails": taskOutput,
		"Notes":       notesOutput,
		"Log":         logOutput,
		"Checkpoints": checkpointsOutput,
	}
	s.templates.ExecuteTemplate(w, "task_detail.html", data)
}

func (s *Server) getCheckpointsForTask(taskID string) string {
	// Find workspace directory for this task
	// Workspaces are named like: 18-slug or use the task seq
	tasksDir := s.oxHome + "/tasks"

	// Get task seq from yoke
	output, err := s.runYoke(fmt.Sprintf("show %s", taskID))
	if err != nil {
		return ""
	}

	// Parse seq from output (e.g., "Task #18 [3dec]")
	var seq int
	if _, err := fmt.Sscanf(output, "Task #%d", &seq); err != nil {
		return ""
	}

	// Find workspace directory starting with seq
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return ""
	}

	var wsDir string
	prefix := fmt.Sprintf("%d-", seq)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			wsDir = tasksDir + "/" + entry.Name()
			break
		}
	}

	if wsDir == "" {
		return ""
	}

	// Run checkpoints from workspace directory
	cmd := exec.Command(s.oxPath, "checkpoints")
	cmd.Dir = wsDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
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

		// Show appropriate action buttons based on status
		buttons := ""
		switch task.Status {
		case "pending":
			buttons = fmt.Sprintf(`
				<button hx-post="/api/start" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh')" class="bg-blue-600 hover:bg-blue-500 px-3 py-1 rounded text-sm">Start</button>
				<button hx-post="/api/done" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh')" class="bg-green-600 hover:bg-green-500 px-2 py-1 rounded text-sm" title="Done">✓</button>
				<button hx-post="/api/drop" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh')" class="bg-gray-600 hover:bg-gray-500 px-2 py-1 rounded text-sm" title="Drop">✗</button>`, task.ID, task.ID, task.ID)
		case "in_progress", "active":
			buttons = fmt.Sprintf(`
				<button hx-post="/api/open" hx-vals='{"task_id":"%d"}' hx-target="#toast" hx-swap="innerHTML" class="bg-green-600 hover:bg-green-500 px-3 py-1 rounded text-sm">Open</button>
				<button hx-post="/api/review" hx-vals='{"task_id":"%d"}' hx-target="#toast" hx-swap="innerHTML" class="bg-purple-600 hover:bg-purple-500 px-3 py-1 rounded text-sm">Review</button>
				<button hx-post="/api/done" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh')" class="bg-emerald-600 hover:bg-emerald-500 px-2 py-1 rounded text-sm" title="Done">✓</button>
				<button hx-post="/api/block" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh')" class="bg-yellow-600 hover:bg-yellow-500 px-2 py-1 rounded text-sm" title="Block">⊘</button>`, task.Seq, task.Seq, task.ID, task.ID)
		case "blocked":
			buttons = fmt.Sprintf(`
				<button hx-post="/api/start" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh')" class="bg-blue-600 hover:bg-blue-500 px-3 py-1 rounded text-sm">Unblock</button>`, task.ID)
		case "done", "dropped":
			buttons = fmt.Sprintf(`
				<button hx-post="/api/reopen" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh')" class="bg-blue-600 hover:bg-blue-500 px-3 py-1 rounded text-sm">Reopen</button>`, task.ID)
		}

		fmt.Fprintf(w, `
		<div class="bg-gray-800 rounded-lg p-4 border border-gray-700 priority-%d hover:border-gray-500 transition">
			<div class="flex items-center justify-between">
				<a href="/task/%s" class="flex items-center space-x-3 hover:text-blue-400">
					<span class="status-%s text-lg">%s</span>
					<span class="text-gray-400 text-sm">#%d</span>
					<span class="font-medium">%s</span>
				</a>
				<div class="flex items-center space-x-2">
					%s
					%s
				</div>
			</div>
		</div>`, task.Priority, task.ID, task.Status, statusIcon, task.Seq, task.Title, tags, buttons)
	}
}

func (s *Server) apiTasksDone(w http.ResponseWriter, r *http.Request) {
	output, err := s.runYoke("list --all")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allTasks := s.parseTaskList(output)

	// Filter for done/dropped tasks
	var doneTasks []Task
	for _, t := range allTasks {
		if t.Status == "done" || t.Status == "dropped" {
			doneTasks = append(doneTasks, t)
		}
	}

	w.Header().Set("Content-Type", "text/html")
	if len(doneTasks) == 0 {
		fmt.Fprintf(w, `<div class="text-gray-500 text-sm py-2">No completed tasks</div>`)
		return
	}

	for _, task := range doneTasks {
		statusIcon := "✓"
		statusColor := "text-green-400"
		if task.Status == "dropped" {
			statusIcon = "✗"
			statusColor = "text-gray-400"
		}

		tags := ""
		for _, tag := range task.Tags {
			tags += fmt.Sprintf(`<span class="bg-gray-700 text-gray-400 px-2 py-0.5 rounded text-xs mr-1">%s</span>`, tag)
		}

		fmt.Fprintf(w, `
		<div class="bg-gray-750 rounded-lg p-3 border border-gray-700 opacity-75">
			<div class="flex items-center justify-between">
				<div class="flex items-center space-x-3">
					<span class="%s">%s</span>
					<span class="text-gray-500 text-sm">#%d</span>
					<span class="text-gray-400">%s</span>
				</div>
				<div class="flex items-center space-x-2">
					%s
					<button hx-post="/api/reopen" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" hx-on::after-request="htmx.trigger('#task-list', 'refresh'); htmx.trigger('#done-list', 'refresh')" class="bg-gray-600 hover:bg-gray-500 px-2 py-1 rounded text-xs">Reopen</button>
				</div>
			</div>
		</div>`, statusColor, statusIcon, task.Seq, task.Title, tags, task.ID)
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

		// Show appropriate button based on status
		button := ""
		icon := "🎯"
		switch task.Status {
		case "pending":
			button = fmt.Sprintf(`<button hx-post="/api/start" hx-vals='{"task_id":"%s"}' hx-target="#toast" hx-swap="innerHTML" class="bg-green-600 hover:bg-green-500 px-3 py-1 rounded text-sm">Start</button>`, task.ID)
		case "in_progress", "active":
			icon = "●"
			button = fmt.Sprintf(`<button hx-post="/api/open" hx-vals='{"task_id":"%d"}' hx-target="#toast" hx-swap="innerHTML" class="bg-blue-600 hover:bg-blue-500 px-3 py-1 rounded text-sm">Open</button>`, task.Seq)
		}

		fmt.Fprintf(w, `
		<div class="bg-gray-800 rounded-lg p-4 border border-gray-700 priority-%d hover:border-green-500 transition cursor-pointer">
			<div class="flex items-center justify-between">
				<div class="flex items-center space-x-3">
					<span class="text-green-500 text-lg">%s</span>
					<span class="text-gray-400 text-sm">#%d</span>
					<span class="font-medium">%s</span>
				</div>
				<div class="flex items-center space-x-2">
					%s
					%s
				</div>
			</div>
		</div>`, task.Priority, icon, task.Seq, task.Title, tags, button)
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
	_, err := s.runYoke(fmt.Sprintf("done %s", taskID))
	w.Header().Set("Content-Type", "text/html")
	if err != nil {
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">Failed: %s</div>`, err.Error())
		return
	}
	fmt.Fprintf(w, `<div class="bg-green-600 text-white px-4 py-2 rounded-lg shadow-lg">Marked #%s as done</div>`, taskID)
}

func (s *Server) apiBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	// Mark as blocked (self-blocked for now)
	_, err := s.runYoke(fmt.Sprintf("block %s --by %s", taskID, taskID))
	w.Header().Set("Content-Type", "text/html")
	if err != nil {
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">Failed: %s</div>`, err.Error())
		return
	}
	fmt.Fprintf(w, `<div class="bg-yellow-600 text-white px-4 py-2 rounded-lg shadow-lg">Marked #%s as blocked</div>`, taskID)
}

func (s *Server) apiDrop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	_, err := s.runYoke(fmt.Sprintf("drop %s", taskID))
	w.Header().Set("Content-Type", "text/html")
	if err != nil {
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">Failed: %s</div>`, err.Error())
		return
	}
	fmt.Fprintf(w, `<div class="bg-gray-600 text-white px-4 py-2 rounded-lg shadow-lg">Dropped #%s</div>`, taskID)
}

func (s *Server) apiReopen(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	_, err := s.runYoke(fmt.Sprintf("edit %s --status pending", taskID))
	w.Header().Set("Content-Type", "text/html")
	if err != nil {
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">Failed: %s</div>`, err.Error())
		return
	}
	fmt.Fprintf(w, `<div class="bg-blue-600 text-white px-4 py-2 rounded-lg shadow-lg">Reopened #%s</div>`, taskID)
}

func (s *Server) apiReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	_, err := s.runOx(fmt.Sprintf("review %s", taskID))
	w.Header().Set("Content-Type", "text/html")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not in a workspace") {
			fmt.Fprintf(w, `<div class="bg-yellow-600 text-white px-4 py-2 rounded-lg shadow-lg">No workspace for #%s</div>`, taskID)
		} else {
			fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">Review failed: check terminal</div>`)
		}
		return
	}
	fmt.Fprintf(w, `<div class="bg-purple-600 text-white px-4 py-2 rounded-lg shadow-lg">Review started for #%s</div>`, taskID)
}

func (s *Server) apiOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	_, err := s.runOx(fmt.Sprintf("open %s", taskID))
	w.Header().Set("Content-Type", "text/html")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "workspace not found") {
			fmt.Fprintf(w, `<div class="bg-yellow-600 text-white px-4 py-2 rounded-lg shadow-lg">No workspace for #%s - run 'ox pickup %s' first</div>`, taskID, taskID)
		} else {
			fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">Failed to open #%s</div>`, taskID)
		}
		return
	}

	fmt.Fprintf(w, `<div class="bg-green-600 text-white px-4 py-2 rounded-lg shadow-lg">Opened #%s in IDE</div>`, taskID)
}

func (s *Server) apiStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	_, err := s.runYoke(fmt.Sprintf("start %s", taskID))
	w.Header().Set("Content-Type", "text/html")
	if err != nil {
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">Failed to start: %s</div>`, err.Error())
		return
	}

	fmt.Fprintf(w, `<div class="bg-blue-600 text-white px-4 py-2 rounded-lg shadow-lg">Started #%s</div>`, taskID)
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

// Feedback authentication

func (s *Server) feedbackAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if password is configured
		if s.config == nil || s.config.FeedbackPassword == "" {
			next(w, r)
			return
		}

		// Check session cookie
		cookie, err := r.Cookie("feedback_session")
		if err == nil {
			s.sessionMu.RLock()
			valid := s.sessions[cookie.Value]
			s.sessionMu.RUnlock()
			if valid {
				next(w, r)
				return
			}
		}

		// Redirect to login
		http.Redirect(w, r, "/feedback/login", http.StatusSeeOther)
	}
}

func (s *Server) handleFeedbackLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		password := r.FormValue("password")
		if s.config != nil && password == s.config.FeedbackPassword {
			// Generate session token
			token := make([]byte, 32)
			rand.Read(token)
			sessionID := hex.EncodeToString(token)

			s.sessionMu.Lock()
			s.sessions[sessionID] = true
			s.sessionMu.Unlock()

			http.SetCookie(w, &http.Cookie{
				Name:     "feedback_session",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				MaxAge:   86400 * 7, // 7 days
			})

			http.Redirect(w, r, "/feedback", http.StatusSeeOther)
			return
		}

		// Wrong password
		s.templates.ExecuteTemplate(w, "feedback_login.html", map[string]interface{}{
			"Error": "Invalid password",
		})
		return
	}

	s.templates.ExecuteTemplate(w, "feedback_login.html", nil)
}

// Feedback page handlers

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	// Get cycle from query param, default to current
	selectedCycle := r.URL.Query().Get("cycle")
	currentCycle := s.feedbackStore.GetCurrentCyclePublic()
	if selectedCycle == "" {
		selectedCycle = currentCycle
	}

	summaries, _ := s.feedbackStore.GetPersonSummariesForCycle(selectedCycle)
	pending, _ := s.feedbackStore.GetPendingWeeks()
	currentWeek := s.feedbackStore.GetCurrentWeek()
	cycles, _ := s.feedbackStore.GetAvailableCycles()

	data := map[string]interface{}{
		"Title":         "Feedback",
		"Active":        "feedback",
		"Summaries":     summaries,
		"PendingWeeks":  len(pending),
		"CurrentWeek":   currentWeek,
		"Cycles":        cycles,
		"SelectedCycle": selectedCycle,
		"CurrentCycle":  currentCycle,
	}
	s.templates.ExecuteTemplate(w, "feedback.html", data)
}

func (s *Server) handleFeedbackWeek(w http.ResponseWriter, r *http.Request) {
	currentWeek := s.feedbackStore.GetCurrentWeek()
	people, _ := s.feedbackStore.LoadPeople()
	tasks := s.parseTaskList(s.getYokeList())

	// Generate recent weeks for backfilling (last 4 weeks)
	recentWeeks := s.feedbackStore.GetRecentWeeks(4)

	data := map[string]interface{}{
		"Title":       "Weekly Review",
		"Active":      "feedback",
		"CurrentWeek": currentWeek,
		"RecentWeeks": recentWeeks,
		"People":      people.People,
		"Tasks":       tasks,
	}
	s.templates.ExecuteTemplate(w, "feedback_week.html", data)
}

func (s *Server) handleFeedbackPrepare(w http.ResponseWriter, r *http.Request) {
	// Get cycle from query param, default to current
	selectedCycle := r.URL.Query().Get("cycle")
	currentCycle := s.feedbackStore.GetCurrentCyclePublic()
	if selectedCycle == "" {
		selectedCycle = currentCycle
	}

	summaries, _ := s.feedbackStore.GetPersonSummariesForCycle(selectedCycle)
	cycles, _ := s.feedbackStore.GetAvailableCycles()

	data := map[string]interface{}{
		"Title":         "Prepare Feedback",
		"Active":        "feedback",
		"Summaries":     summaries,
		"Cycles":        cycles,
		"SelectedCycle": selectedCycle,
		"CurrentCycle":  currentCycle,
	}
	s.templates.ExecuteTemplate(w, "feedback_prepare.html", data)
}

func (s *Server) handleFeedbackPerson(w http.ResponseWriter, r *http.Request) {
	personID := strings.TrimPrefix(r.URL.Path, "/feedback/person/")
	person, err := s.feedbackStore.GetPerson(personID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Get cycle from query param, default to current
	selectedCycle := r.URL.Query().Get("cycle")
	currentCycle := s.feedbackStore.GetCurrentCyclePublic()
	if selectedCycle == "" {
		selectedCycle = currentCycle
	}

	observations, _ := s.feedbackStore.GetObservationsForPersonInCycle(personID, selectedCycle)
	cycles, _ := s.feedbackStore.GetAvailableCycles()

	data := map[string]interface{}{
		"Title":         person.Name,
		"Active":        "feedback",
		"Person":        person,
		"Observations":  observations,
		"Cycles":        cycles,
		"SelectedCycle": selectedCycle,
		"CurrentCycle":  currentCycle,
	}
	s.templates.ExecuteTemplate(w, "feedback_person.html", data)
}

func (s *Server) getYokeList() string {
	output, _ := s.runYoke("list")
	return output
}

// Feedback API handlers

func (s *Server) apiFeedbackPeople(w http.ResponseWriter, r *http.Request) {
	people, err := s.feedbackStore.LoadPeople()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(people.People)
}

func (s *Server) apiFeedbackPeopleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("person_search")
	people, err := s.feedbackStore.SearchPeople(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if len(people) == 0 {
		fmt.Fprintf(w, `<div class="p-2 text-gray-400">No matches. <button onclick="showNewPerson('%s')" class="text-blue-400 underline">Add new?</button></div>`, query)
		return
	}

	for _, p := range people {
		fmt.Fprintf(w, `<div class="p-2 hover:bg-gray-700 cursor-pointer" onclick="selectPerson('%s', '%s')">%s</div>`, p.ID, p.Name, p.Name)
	}
}

func (s *Server) apiFeedbackPeopleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.FormValue("name")
	team := r.FormValue("team")

	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	person, err := s.feedbackStore.AddPerson(name, team)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(person)
}

func (s *Server) apiFeedbackObservations(w http.ResponseWriter, r *http.Request) {
	personID := r.URL.Query().Get("person_id")
	week := r.URL.Query().Get("week")
	var observations []feedback.Observation
	var err error

	if personID != "" {
		observations, err = s.feedbackStore.GetObservationsForPerson(personID)
	} else {
		observations, err = s.feedbackStore.LoadObservations()
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter by week if specified
	if week != "" {
		var filtered []feedback.Observation
		for _, obs := range observations {
			if obs.Week == week {
				filtered = append(filtered, obs)
			}
		}
		observations = filtered
	}

	// Return HTML for HTMX
	w.Header().Set("Content-Type", "text/html")
	if len(observations) == 0 {
		fmt.Fprintf(w, `<p class="text-gray-400 text-center py-4">No observations this week yet</p>`)
		return
	}

	for _, obs := range observations {
		// Get person name
		person, _ := s.feedbackStore.GetPerson(obs.PersonID)
		personName := obs.PersonID
		if person != nil {
			personName = person.Name
		}

		borderColor := "border-green-500"
		typeLabel := "Strength"
		typeColor := "text-green-400"
		if obs.Type == "growth" {
			borderColor = "border-orange-500"
			typeLabel = "Growth Area"
			typeColor = "text-orange-400"
		}

		fmt.Fprintf(w, `
		<div class="bg-gray-700 rounded-lg p-4 border-l-4 %s mb-3">
			<div class="flex items-center justify-between mb-2">
				<span class="font-medium text-white">%s</span>
				<span class="%s text-sm">%s</span>
			</div>
			<p class="text-gray-200 whitespace-pre-wrap text-sm">%s</p>
		</div>`, borderColor, personName, typeColor, typeLabel, obs.Text)
	}
}

func (s *Server) apiFeedbackObservationAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	personID := r.FormValue("person_id")
	text := r.FormValue("text")
	obsType := r.FormValue("type")
	taskID := r.FormValue("task_id")
	week := r.FormValue("week")

	if personID == "" || text == "" || obsType == "" {
		http.Error(w, "person_id, text, and type are required", http.StatusBadRequest)
		return
	}

	obs := feedback.Observation{
		PersonID: personID,
		Text:     text,
		Type:     obsType,
		TaskID:   taskID,
		Week:     week, // Will default to current week if empty
	}

	// Get task info if task_id provided
	if taskID != "" {
		tasks := s.parseTaskList(s.getYokeList())
		for _, t := range tasks {
			if t.ID == taskID {
				obs.TaskSeq = t.Seq
				obs.TaskTitle = t.Title
				break
			}
		}
	}

	if err := s.feedbackStore.AddObservation(obs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="bg-green-600 text-white px-4 py-2 rounded-lg shadow-lg">Observation added</div>`)
}

func (s *Server) apiFeedbackWeekComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	week := r.FormValue("week")
	if week == "" {
		week = s.feedbackStore.GetCurrentWeek()
	}

	// Get tasks worked from form
	var tasks []string
	if taskList := r.FormValue("tasks"); taskList != "" {
		tasks = strings.Split(taskList, ",")
	}

	if err := s.feedbackStore.CompleteWeekReview(week, tasks); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="bg-green-600 text-white px-4 py-2 rounded-lg shadow-lg">Week %s marked complete</div>`, week)
}

func (s *Server) apiFeedbackSummaries(w http.ResponseWriter, r *http.Request) {
	// Use selected cycle from query param, default to current
	cycle := r.URL.Query().Get("cycle")
	if cycle == "" {
		cycle = s.feedbackStore.GetCurrentCyclePublic()
	}

	summaries, err := s.feedbackStore.GetPersonSummariesForCycle(cycle)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if len(summaries) == 0 {
		fmt.Fprintf(w, `<div class="text-gray-400 text-center py-8">No observations yet this cycle</div>`)
		return
	}

	for _, s := range summaries {
		fmt.Fprintf(w, `
		<a href="/feedback/person/%s" class="block bg-gray-800 rounded-lg p-4 border border-gray-700 hover:border-blue-500 transition">
			<div class="flex items-center justify-between">
				<div>
					<div class="font-medium text-lg">%s</div>
					<div class="text-gray-400 text-sm">%s</div>
				</div>
				<div class="flex items-center space-x-4">
					<div class="text-green-400">+%d</div>
					<div class="text-orange-400">~%d</div>
					<div class="text-gray-400">%d total</div>
				</div>
			</div>
		</a>`, s.Person.ID, s.Person.Name, s.Person.Team, len(s.Strengths), len(s.GrowthAreas), s.TotalCount)
	}
}

func (s *Server) apiFeedbackTasks(w http.ResponseWriter, r *http.Request) {
	tasks := s.parseTaskList(s.getYokeList())

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<option value="">No task</option>`)
	for _, t := range tasks {
		fmt.Fprintf(w, `<option value="%s">#%d %s</option>`, t.ID, t.Seq, t.Title)
	}
}

func (s *Server) apiFeedbackCycles(w http.ResponseWriter, r *http.Request) {
	cycles, err := s.feedbackStore.GetAvailableCycles()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cycles)
}

func (s *Server) apiFeedbackObservationEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	obsID := r.FormValue("id")
	text := r.FormValue("text")
	obsType := r.FormValue("type")

	if obsID == "" || text == "" {
		http.Error(w, "id and text are required", http.StatusBadRequest)
		return
	}

	// Get existing observation
	obs, err := s.feedbackStore.GetObservation(obsID)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">%s</div>`, err.Error())
		return
	}

	// Update fields
	obs.Text = text
	if obsType != "" {
		obs.Type = obsType
	}

	if err := s.feedbackStore.UpdateObservation(*obs); err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">%s</div>`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="bg-green-600 text-white px-4 py-2 rounded-lg shadow-lg">Observation updated</div>`)
}

func (s *Server) apiFeedbackObservationDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	obsID := r.FormValue("id")
	if obsID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := s.feedbackStore.DeleteObservation(obsID); err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg">%s</div>`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="bg-green-600 text-white px-4 py-2 rounded-lg shadow-lg">Observation deleted</div>`)
}
