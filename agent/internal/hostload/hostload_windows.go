//go:build windows

package hostload

import (
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pdh                              = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQueryW                = pdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounterW        = pdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData          = pdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterArrayW = pdh.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery                = pdh.NewProc("PdhCloseQuery")
)

const (
	pdhFmtDouble   = 0x00000200
	pdhFmtNoCap100 = 0x00008000
	pdhMoreData    = 0x800007D2
)

// pdhFmtCounterValueItem is PDH_FMT_COUNTERVALUE_ITEM_W holding a double.
type pdhFmtCounterValueItem struct {
	name   *uint16
	status uint32
	_      uint32
	value  float64
}

func logf(format string, args ...any) { log.Printf("[hostload] "+format, args...) }

type reader struct {
	query     uintptr
	cpuTotal  uintptr
	processes uintptr
	gpu       uintptr
}

func newReader() (*reader, error) {
	if err := pdh.Load(); err != nil {
		return nil, err
	}
	r := &reader{}
	if st, _, _ := procPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&r.query))); st != 0 {
		return nil, fmt.Errorf("PdhOpenQuery: 0x%x", st)
	}
	add := func(path string, out *uintptr) error {
		p, _ := windows.UTF16PtrFromString(path)
		if st, _, _ := procPdhAddEnglishCounterW.Call(r.query, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(out))); st != 0 {
			return fmt.Errorf("PdhAddEnglishCounter(%s): 0x%x", path, st)
		}
		return nil
	}
	if err := add(`\Processor(_Total)\% Processor Time`, &r.cpuTotal); err != nil {
		r.close()
		return nil, err
	}
	if err := add(`\Process(*)\% Processor Time`, &r.processes); err != nil {
		r.close()
		return nil, err
	}
	// Missing without WDDM 2.x engine counters: CPU only then.
	if err := add(`\GPU Engine(*)\Utilization Percentage`, &r.gpu); err != nil {
		logf("%v", err)
		r.gpu = 0
	}
	// Rate counters need a first collection to diff the next one against.
	procPdhCollectQueryData.Call(r.query)
	return r, nil
}

func (r *reader) close() {
	if r.query != 0 {
		procPdhCloseQuery.Call(r.query)
		r.query = 0
	}
}

// values reads every instance of counter, with its instance names.
func values(counter uintptr, format uint32) ([]pdhFmtCounterValueItem, []string, error) {
	var size, count uint32
	st, _, _ := procPdhGetFormattedCounterArrayW.Call(counter, uintptr(format), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if st == 0 {
		return nil, nil, nil
	}
	if st != pdhMoreData {
		return nil, nil, fmt.Errorf("PdhGetFormattedCounterArray: 0x%x", st)
	}
	itemSize := unsafe.Sizeof(pdhFmtCounterValueItem{})
	// The buffer also holds the instance name strings after the items.
	buf := make([]pdhFmtCounterValueItem, (uintptr(size)+itemSize-1)/itemSize)
	st, _, _ = procPdhGetFormattedCounterArrayW.Call(counter, uintptr(format), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if st != 0 {
		return nil, nil, fmt.Errorf("PdhGetFormattedCounterArray: 0x%x", st)
	}
	items := buf[:count]
	names := make([]string, count)
	for i := range items {
		names[i] = windows.UTF16PtrToString(items[i].name)
	}
	return items, names, nil
}

func (r *reader) read() (Sample, error) {
	if st, _, _ := procPdhCollectQueryData.Call(r.query); st != 0 {
		return Sample{}, fmt.Errorf("PdhCollectQueryData: 0x%x", st)
	}
	var s Sample
	items, _, err := values(r.cpuTotal, pdhFmtDouble)
	if err != nil {
		return Sample{}, err
	}
	if len(items) > 0 {
		s.CPU = items[0].value
	}
	if items, names, err := values(r.processes, pdhFmtDouble|pdhFmtNoCap100); err == nil {
		for i, it := range items {
			if it.status == 0 && isStreamer(names[i]) {
				s.StreamerCPU += it.value
			}
		}
		// Per process, 100% is one core.
		s.StreamerCPU /= float64(runtime.NumCPU())
	}
	if r.gpu != 0 {
		if items, names, err := values(r.gpu, pdhFmtDouble); err == nil {
			s.GPU, s.StreamerGPU = gpuByEngineType(names, items, streamerPIDs())
		}
	}
	return s, nil
}

// gpuByEngineType folds "pid_<pid>_luid_<a>_<b>_phys_<n>_eng_<n>_engtype_<type>"
// instances into per-type utilization: each engine's load summed over the
// processes using it, then the busiest engine of each type.
func gpuByEngineType(names []string, items []pdhFmtCounterValueItem, streamer map[int]bool) (all, own map[string]float64) {
	engineAll := map[string]float64{}
	engineOwn := map[string]float64{}
	engineType := map[string]string{}
	for i, name := range names {
		if items[i].status != 0 {
			continue
		}
		pid, engine, typ, ok := parseGPUInstance(name)
		if !ok {
			continue
		}
		engineType[engine] = typ
		engineAll[engine] += items[i].value
		if streamer[pid] {
			engineOwn[engine] += items[i].value
		}
	}
	all, own = map[string]float64{}, map[string]float64{}
	for engine, typ := range engineType {
		all[typ] = max(all[typ], min(engineAll[engine], 100))
		own[typ] = max(own[typ], min(engineOwn[engine], 100))
	}
	return all, own
}

func parseGPUInstance(name string) (pid int, engine, typ string, ok bool) {
	rest, found := strings.CutPrefix(name, "pid_")
	if !found {
		return 0, "", "", false
	}
	pidStr, rest, found := strings.Cut(rest, "_")
	if !found {
		return 0, "", "", false
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, "", "", false
	}
	engine, typ, found = strings.Cut(rest, "_engtype_")
	if !found {
		return 0, "", "", false
	}
	typ, ok = normalizeEngineType(typ)
	return pid, engine, typ, ok
}

// normalizeEngineType maps the driver's engine names onto the ones the
// benchmark reports: "3d", "encode", "decode", "copy", and "codec" for a
// shared encode+decode block (AMD's "Video Codec 0"). ok is false for the
// rest (compute, security, overlay, ...).
func normalizeEngineType(typ string) (string, bool) {
	t := strings.ToLower(typ)
	switch {
	case t == "3d":
		return "3d", true
	case strings.Contains(t, "encode"):
		return "encode", true
	case strings.Contains(t, "decode"):
		return "decode", true
	case strings.HasPrefix(t, "video codec"):
		return "codec", true
	case t == "copy":
		return "copy", true
	}
	return "", false
}

// streamerPIDs lists the running streamer processes.
func streamerPIDs() map[int]bool {
	out := map[int]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if isStreamer(windows.UTF16ToString(e.ExeFile[:])) {
			out[int(e.ProcessID)] = true
		}
	}
	return out
}
