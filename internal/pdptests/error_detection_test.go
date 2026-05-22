//go:build pdp_full_carveout
// +build pdp_full_carveout

// Curio Core's compile-only ports of upstream Curio PDP tests.
//
// Goal: confirm that tasks/pdpv0's public surface compiles + the simple
// unit tests still pass when invoked from outside the curio module (i.e.,
// when curio-core depends on it as a Go module).
//
// These tests reach INTO the upstream pdpv0 package via Go import paths.
// They are functionally identical to the upstream tests; only the
// package location differs. When the carveout is incomplete, this file
// is the canary that surfaces it.
//
// Source: vendored from upstream Curio integ/task tasks/pdpv0/error_detection_test.go.

package pdptests

import (
	"errors"
	"testing"

	"github.com/filecoin-project/curio/tasks/pdpv0"
)

func TestIsUnrecoverableError_FromCurioCore(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"DataSetPaymentBeyondEndEpoch by selector",
			errors.New("failed to estimate gas: execution reverted: 0xd7c45de5000000000000"), true},
		{"DataSetPaymentAlreadyTerminated by selector",
			errors.New("execution reverted: 0x211a40c0"), true},
		{"unrelated error", errors.New("network timeout"), false},
		{"contract revert is not termination",
			errors.New("execution reverted: 0x96ed3e73"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pdpv0.IsUnrecoverableError(tc.err); got != tc.expected {
				t.Errorf("IsUnrecoverableError(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		})
	}
}

func TestIsContractRevert_FromCurioCore(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"execution reverted",
			errors.New("failed to estimate gas: execution reverted: 0x96ed3e73"), true},
		{"vm execution error",
			errors.New("vm execution error: something went wrong"), true},
		{"filecoin evm exit code 33",
			errors.New("message failed (exit=[33], revert reason=[...])"), true},
		{"retcode 33", errors.New("call failed with RetCode=33"), true},
		{"network timeout is not revert", errors.New("connection timeout"), false},
		{"rpc error is not revert", errors.New("rpc error: server unavailable"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pdpv0.IsContractRevert(tc.err); got != tc.expected {
				t.Errorf("IsContractRevert(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		})
	}
}

func TestCalculateBackoffBlocks_FromCurioCore(t *testing.T) {
	tests := []struct {
		failures int
		expected int
	}{
		{0, 0},
		{1, 100},
		{2, 200},
		{3, 400},
		{4, 800},
		{5, 1600},
		{10, pdpv0.MaxBackoffBlocks},
	}
	for _, tc := range tests {
		if got := pdpv0.CalculateBackoffBlocks(tc.failures); got != tc.expected {
			t.Errorf("CalculateBackoffBlocks(%d) = %d, want %d", tc.failures, got, tc.expected)
		}
	}
}
