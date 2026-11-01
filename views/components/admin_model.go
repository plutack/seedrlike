package components

import (
	"fmt"
	"math"
)

// AdminDashboardData contains only a point-in-time operational snapshot, not job history.
type AdminDashboardData struct {
	UpdatedAt                  string
	DiskAvailable              bool
	DiskFree, DiskTotal        uint64
	QueueAvailable             bool
	BudgetBytes, ReservedBytes int64
	Throughput                 float64
	Stages                     []AdminStage
	Tasks                      []AdminTask
	MetricsAvailable           bool
	Outcomes                   []AdminOutcome
}

type AdminStage struct {
	Name  string
	Count int
}
type AdminTask struct {
	ID, Name, Status, UserID  string
	Size, Reserved, Completed int64
	Speed                     float64
}
type AdminOutcome struct {
	Status      string
	Jobs, Bytes int64
}

func AdminBytes(n int64) string {
	if n < 0 {
		return "Unavailable"
	}
	return AdminDiskBytes(uint64(n))
}

func AdminDiskBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	i := 0
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func AdminSpeed(n float64) string {
	if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) || n >= float64(math.MaxInt64) {
		return "Not available"
	}
	return AdminBytes(int64(n)) + "/s"
}

func AdminCount(n int64) string    { return fmt.Sprintf("%d", n) }
func AdminQueueCount(n int) string { return fmt.Sprintf("%d", n) }
func AdminTaskName(task AdminTask) string {
	if task.Name == "" {
		return "Awaiting torrent metadata"
	}
	return task.Name
}
func AdminOwner(userID string) string {
	if userID == "" {
		return "Guest"
	}
	return userID
}
