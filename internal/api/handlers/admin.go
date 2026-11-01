package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/plutack/seedrlike/internal/auth"
	"github.com/plutack/seedrlike/internal/core/queue"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/internal/jobs"
	"github.com/plutack/seedrlike/views/components"
	"github.com/plutack/seedrlike/views/layouts"
	"golang.org/x/sys/unix"
)

type adminUserLookup interface {
	GetUserByID(context.Context, string) (database.User, error)
}

type AdminHandler struct {
	queries adminUserLookup
	queue   *queue.DownloadQueue
	store   *jobs.Store
	dataDir string
}

func NewAdminHandler(queries *database.Queries, q *queue.DownloadQueue, store *jobs.Store, dataDir string) *AdminHandler {
	h := &AdminHandler{queue: q, store: store, dataDir: dataDir}
	// Keep an absent database distinguishable from a typed-nil interface.
	if queries != nil {
		h.queries = queries
	}
	return h
}

// authorizeAdmin resolves the authenticated identity from the database. Query
// parameters and headers are deliberately never used for identity or roles.
func authorizeAdmin(ctx context.Context, queries adminUserLookup) int {
	userID, ok := ctx.Value(UserIDKey).(string)
	if !ok || userID == "" {
		return http.StatusUnauthorized
	}
	if queries == nil {
		return http.StatusServiceUnavailable
	}
	user, err := queries.GetUserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return http.StatusUnauthorized
	}
	if err != nil {
		return http.StatusServiceUnavailable
	}
	if !auth.IsAdmin(user.Username) {
		return http.StatusForbidden
	}
	return http.StatusOK
}

// Dashboard serves GET /admin. All sensitive reads happen after authorization.
// Refresh is a normal navigation, so no full-page HTMX polling is required.
func (h *AdminHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if status := authorizeAdmin(r.Context(), h.queries); status != http.StatusOK {
		http.Error(w, http.StatusText(status), status)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data := components.AdminDashboardData{UpdatedAt: time.Now().UTC().Format("2006-01-02 15:04:05 UTC")}
	var disk unix.Statfs_t
	if h.dataDir != "" {
		if err := unix.Statfs(h.dataDir, &disk); err == nil && disk.Bsize > 0 {
			data.DiskAvailable = true
			// Bavail is usable by the process, unlike Bfree which includes root reserves.
			data.DiskFree = disk.Bavail * uint64(disk.Bsize)
			data.DiskTotal = disk.Blocks * uint64(disk.Bsize)
		} else if err != nil {
			slog.Error("Admin disk capacity unavailable", "operation", "admin_disk_capacity", "reason", "filesystem_query_failed", "error_type", fmt.Sprintf("%T", err))
		}
	}
	if h.queue != nil {
		snapshot := h.queue.Snapshot()
		data.QueueAvailable = true
		data.BudgetBytes, data.ReservedBytes = snapshot.BudgetBytes, snapshot.ReservedBytes
		counts := map[string]int{}
		for _, task := range snapshot.Tasks {
			counts[task.Status]++
			data.Tasks = append(data.Tasks, components.AdminTask{ID: task.ID, Name: task.Name, Status: task.Status, UserID: task.UserID, Size: task.Size, Reserved: task.Reserved, Completed: task.Completed, Speed: task.Speed})
			if task.Speed > 0 && !math.IsNaN(task.Speed) && !math.IsInf(task.Speed, 0) {
				data.Throughput += task.Speed
			}
		}
		for stage, count := range counts {
			data.Stages = append(data.Stages, components.AdminStage{Name: stage, Count: count})
		}
		sort.Slice(data.Stages, func(i, j int) bool { return data.Stages[i].Name < data.Stages[j].Name })
		sort.Slice(data.Tasks, func(i, j int) bool { return data.Tasks[i].ID < data.Tasks[j].ID })
	}
	if h.store != nil {
		metrics, err := h.store.Metrics(r.Context())
		if err != nil {
			slog.Error("Admin outcome metrics unavailable", "operation", "admin_outcome_metrics", "reason", "database_query_failed", "error_type", fmt.Sprintf("%T", err))
		} else {
			data.MetricsAvailable = true
			// Include meaningful zeros only after a successful metrics query.
			outcomes := map[string]components.AdminOutcome{
				"completed": {Status: "completed"}, "failed": {Status: "failed"}, "stopped": {Status: "stopped"},
			}
			for _, metric := range metrics {
				outcomes[metric.Status] = components.AdminOutcome{Status: metric.Status, Jobs: metric.Jobs, Bytes: metric.Bytes}
			}
			for _, outcome := range outcomes {
				data.Outcomes = append(data.Outcomes, outcome)
			}
			sort.Slice(data.Outcomes, func(i, j int) bool { return data.Outcomes[i].Status < data.Outcomes[j].Status })
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := layouts.SettingsPage("Capacity dashboard", true, components.AdminDashboard(data)).Render(r.Context(), w); err != nil {
		slog.Error("Admin dashboard render failed", "operation", "render_admin_dashboard", "reason", "render_failed", "error_type", fmt.Sprintf("%T", err))
	}
}
