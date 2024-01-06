package abi

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// PackEvent packs the given event name and arguments to conform the ABI.
// Returns the topics for the event including the event signature (if non-anonymous event) and
// hashes derived from indexed arguments and the packed data of non-indexed args according to
// the event ABI specification.
// The order of arguments must match the order of the event definition.
// https://docs.soliditylang.org/en/v0.8.17/abi-spec.html#indexed-event-encoding.
// Note: PackEvent does not support array (fixed or dynamic-size) or struct types.
func (abi ABI) PackEvent(name string, args ...interface{}) ([]common.Hash, []byte, error) {
	event, exist := abi.Events[name]
	if !exist {
		return nil, nil, fmt.Errorf("event '%s' not found", name)
	}
	if len(args) != len(event.Inputs) {
		return nil, nil, fmt.Errorf("event '%s' unexpected number of inputs %d", name, len(args))
	}

	var (
		nonIndexedInputs = make([]interface{}, 0)
		indexedInputs    = make([]interface{}, 0)
		nonIndexedArgs   Arguments
		indexedArgs      Arguments
	)

	for i, arg := range event.Inputs {
		if arg.Indexed {
			indexedArgs = append(indexedArgs, arg)
			indexedInputs = append(indexedInputs, args[i])
		} else {
			nonIndexedArgs = append(nonIndexedArgs, arg)
			nonIndexedInputs = append(nonIndexedInputs, args[i])
		}
	}

	packedArguments, err := nonIndexedArgs.Pack(nonIndexedInputs...)
	if err != nil {
		return nil, nil, err
	}
	topics := make([]common.Hash, 0, len(indexedArgs)+1)
	if !event.Anonymous {
		topics = append(topics, event.ID)
	}
	indexedTopics, err := PackTopics(indexedInputs)
	if err != nil {
		return nil, nil, err
	}

	return append(topics, indexedTopics...), packedArguments, nil
}

// PackOutput packs the given [args] as the output of given method [name] to conform the ABI.
// This does not include method ID.
func (abi ABI) PackOutput(name string, args ...interface{}) ([]byte, error) {
	// Fetch the ABI of the requested method
	method, exist := abi.Methods[name]
	if !exist {
		return nil, fmt.Errorf("method '%s' not found", name)
	}
	arguments, err := method.Outputs.Pack(args...)
	if err != nil {
		return nil, err
	}
	return arguments, nil
}

// getInputs gets input arguments of the given [name] method.
func (abi ABI) getInputs(name string, data []byte, useStrictMode bool) (Arguments, error) {
	// since there can't be naming collisions with contracts and events,
	// we need to decide whether we're calling a method or an event
	var args Arguments
	if method, ok := abi.Methods[name]; ok {
		if useStrictMode && len(data)%32 != 0 {
			return nil, fmt.Errorf("abi: improperly formatted input: %s - Bytes: [%+v]", string(data), data)
		}
		args = method.Inputs
	}
	if event, ok := abi.Events[name]; ok {
		args = event.Inputs
	}
	if args == nil {
		return nil, fmt.Errorf("abi: could not locate named method or event: %s", name)
	}
	return args, nil
}

// UnpackInput unpacks the input according to the ABI specification.
func (abi ABI) UnpackInput(name string, data []byte, useStrictMode bool) ([]interface{}, error) {
	args, err := abi.getInputs(name, data, useStrictMode)
	if err != nil {
		return nil, err
	}
	return args.Unpack(data)
}

// UnpackInputIntoInterface unpacks the input in v according to the ABI specification.
// It performs an additional copy. Please only use, if you want to unpack into a
// structure that does not strictly conform to the ABI structure (e.g. has additional arguments)
func (abi ABI) UnpackInputIntoInterface(v interface{}, name string, data []byte, useStrictMode bool) error {
	args, err := abi.getInputs(name, data, useStrictMode)
	if err != nil {
		return err
	}
	unpacked, err := args.Unpack(data)
	if err != nil {
		return err
	}
	return args.Copy(v, unpacked)
}
