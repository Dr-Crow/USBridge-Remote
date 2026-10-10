package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestGraphicsEnvironmentOnlyFixedSoftwarePolicy(t *testing.T) {
	var removed []string
	input := []string{"GALLIUM_DRIVER=zink", "mesa_log_file=private", "LIBGL_ALWAYS_SOFTWARE=false", "LP_NUM_THREADS=300", "LLVM_PIPE=private", "WGL_X=x", "ZINK_DEBUG=x", "D3D12_ADAPTER_NAME=x", "SystemRoot=C:\\Windows", "DISPLAY=:99", "PATH=kept"}
	if err := clearGraphicsEnvironmentWith(input, func(k string) error { removed = append(removed, k); return nil }); err != nil {
		t.Fatal(err)
	}
	want := []string{"GALLIUM_DRIVER", "mesa_log_file", "LIBGL_ALWAYS_SOFTWARE", "LP_NUM_THREADS", "LLVM_PIPE", "WGL_X", "ZINK_DEBUG", "D3D12_ADAPTER_NAME"}
	if !reflect.DeepEqual(removed, want) {
		t.Fatal("unexpected graphics override selection")
	}
	values := map[string]string{}
	if err := setSoftwareGraphicsWith(func(k, v string) error { values[k] = v; return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, map[string]string{"GALLIUM_DRIVER": "llvmpipe", "LIBGL_ALWAYS_SOFTWARE": "true"}) {
		t.Fatal("unbounded graphics policy")
	}
}

func TestGraphicsEnvironmentFailureStopsConfiguration(t *testing.T) {
	failure := errors.New("fixed fixture failure")
	if err := clearGraphicsEnvironmentWith([]string{"MESA_LOG_FILE=x"}, func(string) error { return failure }); err != failure {
		t.Fatal("erase failure lost")
	}
	for failAt := 1; failAt <= 2; failAt++ {
		calls := 0
		err := setSoftwareGraphicsWith(func(string, string) error {
			calls++
			if calls == failAt {
				return failure
			}
			return nil
		})
		if err != failure || calls != failAt {
			t.Fatal("configuration failure lost")
		}
	}
}
