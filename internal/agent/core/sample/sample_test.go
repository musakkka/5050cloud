// SPDX-License-Identifier: AGPL-3.0-only

package sample

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) Roots {
	root := t.TempDir()
	r := Roots{Proc: filepath.Join(root, "proc"), Cgroup: filepath.Join(root, "cgroup")}
	write(t, filepath.Join(r.Proc, "stat"), `cpu  100 0 100 700 100 0 0 0 0 0
cpu0 50 0 50 350 50 0 0 0 0 0
cpu1 50 0 50 350 50 0 0 0 0 0
intr 1
btime 1700000000
`)
	write(t, filepath.Join(r.Proc, "meminfo"), "MemTotal:        2048000 kB\nMemFree:          100000 kB\nMemAvailable:    1024000 kB\n")
	write(t, filepath.Join(r.Proc, "loadavg"), "0.75 0.50 0.25 1/100 999\n")
	write(t, filepath.Join(r.Proc, "4242", "cgroup"), "0::/5050-apps.slice/docker-abc.scope\n")
	write(t, filepath.Join(r.Cgroup, "5050-apps.slice", "docker-abc.scope", "cpu.stat"), "usage_usec 3000000\nuser_usec 2000000\n")
	write(t, filepath.Join(r.Cgroup, "5050-apps.slice", "docker-abc.scope", "memory.current"), "52428800\n")
	write(t, filepath.Join(r.Cgroup, "5050-apps.slice", "docker-abc.scope", "memory.max"), "536870912\n")
	write(t, filepath.Join(r.Proc, "4242", "net", "dev"), `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  999999     100    0    0    0     0          0         0   999999     100    0    0    0     0       0          0
  eth0:    1500      10    0    0    0     0          0         0     2500      20    0    0    0     0       0          0
  eth1:     500       1    0    0    0     0          0         0      500       1    0    0    0     0       0          0
`)
	// 0.0.0.0:3000 listen, 127.0.0.1:9229 listen, 127.0.0.1:5432 established, [::]:8080 listen, [::1]:6000 listen
	write(t, filepath.Join(r.Proc, "4242", "net", "tcp"), `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 100 0 0 10 0
   1: 0100007F:240D 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1538 0100007F:9C40 01 00000000:00000000 00:00000000 00000000     0        0 3 1 0000000000000000 100 0 0 10 0
`)
	write(t, filepath.Join(r.Proc, "4242", "net", "tcp6"), `  sl  local_address                         remote_address                        st
   0: 00000000000000000000000000000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4 1
   1: 00000000000000000000000001000000:1770 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 5 1
   2: zz:1770 00000000000000000000000000000000:0000 0A
`)
	return r
}

func TestHost(t *testing.T) {
	r := fixture(t)
	cur, cpus, err := r.ReadCPU()
	if err != nil || cpus != 2 || cur.Total != 1000 || cur.Idle != 800 {
		t.Fatalf("%+v %d %v", cur, cpus, err)
	}
	prev := CPUTimes{Total: 500, Idle: 450}
	if got := HostCPU(prev, cur, cpus); math.Abs(got-0.6) > 1e-9 {
		t.Fatalf("vCPU %v, want 0.6", got)
	}
	if HostCPU(cur, cur, 2) != 0 {
		t.Fatal("no delta")
	}
	bt, err := r.BootTime()
	if err != nil || bt.Unix() != 1700000000 {
		t.Fatal(bt, err)
	}
	total, used, err := r.Memory()
	if err != nil || total != 2048000*1024 || used != 1024000*1024 {
		t.Fatal(total, used, err)
	}
	if r.Load1() != 0.75 {
		t.Fatal(r.Load1())
	}
	bad := Roots{Proc: t.TempDir()}
	if _, _, err := bad.ReadCPU(); err == nil {
		t.Fatal("missing stat")
	}
	if _, err := bad.BootTime(); err == nil {
		t.Fatal("missing stat")
	}
	if _, _, err := bad.Memory(); err == nil {
		t.Fatal("missing meminfo")
	}
	if bad.Load1() != 0 {
		t.Fatal("missing loadavg")
	}
	write(t, filepath.Join(bad.Proc, "stat"), "intr 1\n")
	write(t, filepath.Join(bad.Proc, "meminfo"), "MemFree: 1 kB\n")
	write(t, filepath.Join(bad.Proc, "loadavg"), "")
	if _, _, err := bad.ReadCPU(); err == nil {
		t.Fatal("no cpu line")
	}
	if _, err := bad.BootTime(); err == nil {
		t.Fatal("no btime")
	}
	if _, _, err := bad.Memory(); err == nil {
		t.Fatal("no MemTotal")
	}
}

func TestContainer(t *testing.T) {
	r := fixture(t)
	dir, err := r.CgroupPath(4242)
	if err != nil || dir != filepath.Join(r.Cgroup, "5050-apps.slice/docker-abc.scope") {
		t.Fatal(dir, err)
	}
	u, err := ReadCgroup(dir)
	if err != nil || u.CPUUsec != 3000000 || u.MemBytes != 52428800 || u.MemMax != 536870912 {
		t.Fatalf("%+v %v", u, err)
	}
	write(t, filepath.Join(dir, "memory.max"), "max\n")
	if u, _ := ReadCgroup(dir); u.MemMax != 0 {
		t.Fatal("max means unlimited")
	}
	if _, err := ReadCgroup(t.TempDir()); err == nil {
		t.Fatal("missing cpu.stat")
	}
	rx, tx, err := r.NetDev(4242)
	if err != nil || rx != 2000 || tx != 3000 {
		t.Fatal(rx, tx, err)
	}
	if _, _, err := r.NetDev(1); err == nil {
		t.Fatal("missing pid")
	}
	l, lb, err := r.Ports(4242)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l, []int{3000, 8080}) || !reflect.DeepEqual(lb, []int{6000, 9229}) {
		t.Fatalf("listening %v loopback %v", l, lb)
	}
	if _, _, err := r.Ports(1); err == nil {
		t.Fatal("missing pid")
	}
	write(t, filepath.Join(r.Proc, "77", "cgroup"), "12:cpu:/x\n")
	if _, err := r.CgroupPath(77); err == nil {
		t.Fatal("cgroup v1 only")
	}
	if _, err := r.CgroupPath(78); err == nil {
		t.Fatal("missing")
	}
}

func TestCPUTracker(t *testing.T) {
	tr := NewCPUTracker()
	t0 := time.Unix(100, 0)
	if tr.VCPU("a", 1_000_000, t0) != 0 {
		t.Fatal("first sample")
	}
	if got := tr.VCPU("a", 16_000_000, t0.Add(10*time.Second)); math.Abs(got-1.5) > 1e-9 {
		t.Fatal(got)
	}
	if tr.VCPU("a", 1, t0.Add(20*time.Second)) != 0 {
		t.Fatal("counter reset gives 0")
	}
	tr.Forget(map[string]bool{})
	if tr.VCPU("a", 5, t0.Add(30*time.Second)) != 0 {
		t.Fatal("forgotten key starts over")
	}
}

func TestDirSizeAndStatfs(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), "12345")
	write(t, filepath.Join(dir, "b", "c"), "123")
	n, complete, err := DirSize(dir, time.Second)
	if err != nil || !complete || n != 8 {
		t.Fatal(n, complete, err)
	}
	if _, complete, _ := DirSize(dir, -time.Second); complete {
		t.Fatal("an expired budget is incomplete")
	}
	used, total, err := Statfs(dir)
	if err != nil || total <= 0 || used < 0 || used > total {
		t.Fatal(used, total, err)
	}
	if _, _, err := Statfs(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing path")
	}
	if !SameDevice(dir, filepath.Join(dir, "a")) || SameDevice(dir, filepath.Join(dir, "missing")) {
		t.Fatal("SameDevice")
	}
	if _, _, ok := parseHexAddr("nocolon"); ok {
		t.Fatal("bad addr")
	}
	if _, _, ok := parseHexAddr("0100007F:ZZZZ"); ok {
		t.Fatal("bad port")
	}
}
