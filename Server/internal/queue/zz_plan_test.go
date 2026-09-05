package queue

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestExplainPlans(t *testing.T) {
	q, err := NewSQLiteQueue(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	queries := map[string]string{
		"GetPendingJobs": `SELECT ` + jobColumns + ` FROM jobs WHERE status = 'pending' ORDER BY created_at ASC`,
		"GetActiveJobs":  `SELECT ` + jobColumns + ` FROM jobs WHERE status IN ('pending', 'running') ORDER BY created_at ASC`,
		"GetRecentLogs":  `SELECT ` + jobColumns + ` FROM jobs WHERE status = ? ORDER BY completed_at DESC, created_at DESC LIMIT ?`,
		"ResetRunning":   `SELECT ` + jobColumns + ` FROM jobs WHERE status = 'running'`,
	}
	for name, sql := range queries {
		rows, err := q.db.Query("EXPLAIN QUERY PLAN " + sql, argsFor(name)...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		fmt.Printf("--- %s ---\n", name)
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			fmt.Printf("    %s\n", detail)
		}
		rows.Close()
	}
}

func argsFor(name string) []any {
	if name == "GetRecentLogs" {
		return []any{"completed", 50}
	}
	return nil
}
