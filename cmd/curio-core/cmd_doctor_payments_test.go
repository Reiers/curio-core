package main

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestOperatorApprovalsSelector locks the 4-byte selector for FilecoinPay's
// auto-generated public-mapping getter. If this changes, readOperatorApproval
// silently reads the wrong slot. (curio-core#91)
func TestOperatorApprovalsSelector(t *testing.T) {
	got := hex.EncodeToString(selOperatorApprovals)
	const want = "e3d4c69e" // cast sig "operatorApprovals(address,address,address)"
	if got != want {
		t.Fatalf("operatorApprovals selector = %s, want %s", got, want)
	}
}

// TestOperatorApprovalsCalldata verifies the 4 + 96 byte calldata layout
// (selector + 3 left-padded addresses) matches the order
// operatorApprovals(token, client, operator).
func TestOperatorApprovalsCalldata(t *testing.T) {
	token := common.HexToAddress("0xb3042734b608a1B16e9e86B374A3f3e389B4cDf0")
	client := common.HexToAddress("0x5Df2ff00be0c8f320009f9621Adb48B861aB4c52")
	operator := common.HexToAddress("0x02925630df557F957f70E112bA06e50965417CA0")

	data := make([]byte, 0, 4+96)
	data = append(data, selOperatorApprovals...)
	data = append(data, common.LeftPadBytes(token.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(client.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(operator.Bytes(), 32)...)

	if len(data) != 4+96 {
		t.Fatalf("calldata len = %d, want %d", len(data), 4+96)
	}
	// addresses occupy the low 20 bytes of each 32-byte word.
	if got := common.BytesToAddress(data[4+12 : 4+32]); got != token {
		t.Errorf("word0 = %s, want token %s", got, token)
	}
	if got := common.BytesToAddress(data[36+12 : 36+32]); got != client {
		t.Errorf("word1 = %s, want client %s", got, client)
	}
	if got := common.BytesToAddress(data[68+12 : 68+32]); got != operator {
		t.Errorf("word2 = %s, want operator %s", got, operator)
	}
}

// TestOperatorApprovalDecodeFirstWord verifies the isApproved decode: the
// getter flattens the OperatorApproval struct and the first 32-byte word is
// bool isApproved (non-zero = true).
func TestOperatorApprovalDecodeFirstWord(t *testing.T) {
	// approved=true, rateAllowance=max, lockupAllowance=max — only word0 matters.
	approvedRet := make([]byte, 96)
	approvedRet[31] = 1 // bool true in the low byte of word0
	if new(big.Int).SetBytes(approvedRet[:32]).Sign() == 0 {
		t.Error("expected approved=true to decode as non-zero")
	}

	notApprovedRet := make([]byte, 96) // all zero
	if new(big.Int).SetBytes(notApprovedRet[:32]).Sign() != 0 {
		t.Error("expected approved=false to decode as zero")
	}
}
