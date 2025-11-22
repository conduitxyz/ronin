// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Provides support for dealing with EVM assembly instructions (e.g., disassembling them).
package asm

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/core/vm"
)

// instructionIterator iterates over disassembled EVM instructions.
type instructionIterator struct {
	code    []byte
	pc      uint64
	arg     []byte
	op      vm.OpCode
	error   error
	started bool
}

// operandSize returns the number of bytes consumed by the operand of op.
// This handles the PUSH instructions (PUSH1 through PUSH32).
func operandSize(op vm.OpCode) uint64 {
	if op >= vm.PUSH1 && op <= vm.PUSH32 {
		return uint64(op) - uint64(vm.PUSH1) + 1
	}
	return 0
}

// Create a new instruction iterator.
func NewInstructionIterator(code []byte) *instructionIterator {
	it := new(instructionIterator)
	it.code = code
	return it
}

// Returns true if there is a next instruction and moves on.
func (it *instructionIterator) Next() bool {
	if it.error != nil || it.pc >= uint64(len(it.code)) {
		// We previously reached an error or the end.
		return false
	}

	if it.started {
		// Advance PC past the previous opcode (1 byte) and its argument size (if any).
		advance := uint64(1)
		if it.arg != nil {
			advance += uint64(len(it.arg))
		}
		it.pc += advance
	} else {
		// Start the iteration from the first instruction.
		it.started = true
	}

	// Check for end of code after advancing/starting
	if it.pc >= uint64(len(it.code)) {
		return false
	}

	it.op = vm.OpCode(it.code[it.pc])
	
    argSize := operandSize(it.op)

    if argSize > 0 {
		u := it.pc + 1 + argSize
		if u > uint64(len(it.code)) {
			// Argument runs past the end of the code slice.
			it.error = fmt.Errorf("incomplete push instruction at %05x: opcode %v requires %d bytes, but only %d available", 
                                  it.pc, it.op, argSize, uint64(len(it.code)) - (it.pc + 1))
			return false
		}
		// Read argument slice
		it.arg = it.code[it.pc+1 : u]
	} else {
		it.arg = nil
	}
	return true
}

// Returns any error that may have been encountered.
func (it *instructionIterator) Error() error {
	return it.error
}

// Returns the PC of the current instruction.
func (it *instructionIterator) PC() uint64 {
	return it.pc
}

// Returns the opcode of the current instruction.
func (it *instructionIterator) Op() vm.OpCode {
	return it.op
}

// Returns the argument of the current instruction.
func (it *instructionIterator) Arg() []byte {
	return it.arg
}

// Pretty-print all disassembled EVM instructions to stdout.
func PrintDisassembled(code string) error {
	script, err := hex.DecodeString(code)
	if err != nil {
		return err
	}

	it := NewInstructionIterator(script)
	for it.Next() {
		if it.Arg() != nil && 0 < len(it.Arg()) {
			// Note: fmt.Printf uses 0x%x on []byte to print the hex string representation.
			fmt.Printf("%05x: %v 0x%x\n", it.PC(), it.Op(), it.Arg())
		} else {
			fmt.Printf("%05x: %v\n", it.PC(), it.Op())
		}
	}
	return it.Error()
}

// Return all disassembled EVM instructions in human-readable format.
func Disassemble(script []byte) ([]string, error) {
	instrs := make([]string, 0)

	it := NewInstructionIterator(script)
	for it.Next() {
		if it.Arg() != nil && 0 < len(it.Arg()) {
			instrs = append(instrs, fmt.Sprintf("%05x: %v 0x%x\n", it.PC(), it.Op(), it.Arg()))
		} else {
			instrs = append(instrs, fmt.Sprintf("%05x: %v\n", it.PC(), it.Op()))
		}
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return instrs, nil
}
