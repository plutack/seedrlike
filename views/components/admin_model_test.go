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
		{name: "unavailable", want: []string{"Disk capacity unavailable", "Queue capacity unavailable", "Throughput unavailable", "Current jobs unavailable", "Outcome counters unavailable"}, absent: []string{"No current jobs", "No recorded outcomes", "0 B"}},
		{name: "empty", data: AdminDashboardData{DiskAvailable: true, QueueAvailable: true, MetricsAvailable: true}, want: []string{"No current jobs", "No recorded outcomes", "0 B", "Not available"}, absent: []string{"capacity unavailable", "counters unavailable"}},
		{
			name: "available metrics",
			data: AdminDashboardData{
				UpdatedAt: "2026-11-01 12:00 UTC", DiskAvailable: true,
				DiskFree: 1073741824, DiskTotal: 8589934592,
				QueueAvailable: true, BudgetBytes: 4294967296, ReservedBytes: 2147483648, Throughput: 3072,
				Stages:           []AdminStage{{Name: "downloading", Count: 2}, {Name: "pending", Count: 3}},
				Tasks:            []AdminTask{{ID: "job-unique-42", UserID: "owner-unique-17", Name: "Example torrent", Status: "downloading", Completed: 512, Size: 4096, Reserved: 8192, Speed: 2048}},
				MetricsAvailable: true,
				Outcomes:         []AdminOutcome{{Status: "completed", Jobs: 23, Bytes: 16384}, {Status: "failed", Jobs: 7, Bytes: 32768}, {Status: "cancelled", Jobs: 0, Bytes: 0}},
			},
			want:   []string{"2026-11-01 12:00 UTC", "1.0 GiB", "8.0 GiB", "4.0 GiB", "2.0 GiB", "3.0 KiB/s", "Queue stages", "downloading", "pending", ">2</dd>", ">3</dd>", "Example torrent", "512 B", "4.0 KiB", "8.0 KiB", "2.0 KiB/s", "job-unique-42", "owner-unique-17", "Job / owner IDs", "<details", "completed", "failed", "cancelled", ">23</dd>", ">7</dd>", ">0</dd>", "16.0 KiB", "32.0 KiB", "Last 30 days", "Recorded job sizes, not network traffic", "not disk usage", "overflow-x-auto", "tabindex=\"0\"", "scope=\"col\"", "scope=\"row\""},
			absent: []string{"capacity unavailable", "No current jobs", "No recorded outcomes"},
		},
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
			for _, removed := range []string{"Administrator access only", "Zero means no recorded outcomes", "queue snapshot", "Refresh snapshot"} {
				if strings.Contains(html, removed) {
					t.Errorf("redundant dashboard copy retained: %q", removed)
				}
			}
			if strings.Contains(html, "hx-trigger") {
				t.Error("must not poll full-page route")
			}
		})
	}
}
