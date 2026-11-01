package components

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
)

func TestAdminFormatting(t *testing.T) {
	for _, tt := range []struct {
		n    int64
		want string
	}{{0, "0 B"}, {1024, "1.0 KiB"}, {1073741824, "1.0 GiB"}, {-1, "Unavailable"}} {
		if got := AdminBytes(tt.n); got != tt.want {
			t.Errorf("AdminBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
	for _, n := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if got := AdminSpeed(n); got != "Not available" {
			t.Errorf("AdminSpeed(%v) = %q", n, got)
		}
	}
	if got := AdminSpeed(2048); got != "2.0 KiB/s" {
		t.Errorf("speed = %q", got)
	}
}

func TestAdminDashboardStatesAndEscaping(t *testing.T) {
	tests := []struct {
		name   string
		data   AdminDashboardData
		want   []string
		absent []string
	}{
		{name: "unavailable", want: []string{"Disk capacity unavailable", "Queue capacity unavailable", "Current jobs unavailable", "Outcome counters unavailable"}, absent: []string{"0 jobs in the queue"}},
		{name: "empty", data: AdminDashboardData{DiskAvailable: true, QueueAvailable: true, MetricsAvailable: true}, want: []string{"No current jobs", "0 jobs in the queue", "0 B"}},
		{name: "escaped job", data: AdminDashboardData{QueueAvailable: true, Tasks: []AdminTask{{Name: "<script>alert(1)</script>", ID: "j1", Status: "pending"}}}, want: []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "Awaiting metadata", "Guest"}, absent: []string{"<script>alert(1)</script>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := AdminDashboard(tt.data).Render(context.Background(), &output); err != nil {
				t.Fatal(err)
			}
			html := output.String()
			for _, want := range tt.want {
				if !strings.Contains(html, want) {
					t.Errorf("missing %q", want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(html, absent) {
					t.Errorf("unexpected %q", absent)
				}
			}
			if strings.Contains(html, "hx-trigger") {
				t.Error("must not poll full-page route")
			}
		})
	}
}
