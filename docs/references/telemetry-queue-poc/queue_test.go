package priorart

import (
	"os"
	"os/exec"
	diskqueue "priorart/internal/diskqueue"
	"testing"
	"time"
)

func queue(path string) diskqueue.Interface {
	return diskqueue.New("audit", path, 1<<20, 1, 1<<16, 1, 5*time.Millisecond, func(diskqueue.LogLevel, string, ...interface{}) {})
}
func TestCrashWorker(t *testing.T) {
	mode := os.Getenv("PRIORART_CRASH_MODE")
	if mode == "" {
		return
	}
	q := queue(os.Getenv("PRIORART_QUEUE_DIR"))
	if err := q.Put([]byte("report-1")); err != nil {
		panic(err)
	}
	ch := q.ReadChan()
	if mode == "peek" {
		ch = q.PeekChan()
	}
	select {
	case <-time.After(time.Second):
		panic("queue timeout")
	case <-ch:
	}
	// Wait for periodic sync; process crash, not physical power-loss testing.
	time.Sleep(200 * time.Millisecond)
	os.Exit(0)
}
func TestPeekRetainsUnacknowledgedReportAcrossProcessCrash(t *testing.T) { check(t, "peek", 1) }
func TestReadConsumesReportBeforeRemoteAck(t *testing.T)                 { check(t, "read", 0) }
func check(t *testing.T, mode string, want int64) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashWorker$")
	cmd.Env = append(os.Environ(), "PRIORART_CRASH_MODE="+mode, "PRIORART_QUEUE_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worker: %v %s", err, out)
	}
	q := queue(dir)
	defer q.Close()
	if got := q.Depth(); got != want {
		t.Fatalf("%s depth=%d want=%d", mode, got, want)
	}
	if want == 1 {
		select {
		case data := <-q.PeekChan():
			if string(data) != "report-1" {
				t.Fatalf("payload %q", data)
			}
		case <-time.After(time.Second):
			t.Fatal("missing replay")
		}
		<-q.ReadChan()
		if q.Depth() != 0 {
			t.Fatal("ACK did not consume")
		}
	}
}
