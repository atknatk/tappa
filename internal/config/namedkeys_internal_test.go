package config

import (
	"bytes"
	"reflect"
	"testing"
)

// TestNamedKeys_ListEveryKeyFieldOfTheConfig derives the set of keys from the STRUCT,
// not from a list: every []byte field of Config is filled with its own distinct
// value, and namedKeys must hand back each of those values exactly once.
//
// WHY DERIVED (agent-brief, "TÜRET"): operatorKeySeparation compares the operator keys
// against whatever namedKeys returns. A key field added to Config later and not added
// there would be a key the rule never sees -- and the natural repair for a hand-kept
// list going red is to update the list, which is the wrong move when the list is the
// thing that was forgotten. Here a new []byte field is red until it is listed.
//
// It also pins the two `operator` flags: exactly the two operator fields are marked,
// because the flag decides which pairs the rule compares.
func TestNamedKeys_ListEveryKeyFieldOfTheConfig(t *testing.T) {
	var c Config
	rv := reflect.ValueOf(&c).Elem()
	rt := rv.Type()
	byteSlice := reflect.TypeOf([]byte(nil))
	fieldOf := map[string]string{} // value -> field name
	fields := 0
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).Type != byteSlice {
			continue
		}
		fields++
		v := bytes.Repeat([]byte{byte(i + 1)}, 32)
		rv.Field(i).SetBytes(v)
		fieldOf[string(v)] = rt.Field(i).Name
	}
	if fields < 6 {
		t.Fatalf("only %d []byte fields found in Config; the derivation has gone blind (six keys exist today)", fields)
	}

	seen := map[string]int{}
	operators := map[string]bool{}
	for _, k := range c.namedKeys() {
		name, ok := fieldOf[string(k.v)]
		if !ok {
			t.Errorf("namedKeys lists %s with a value no []byte field of Config holds", k.name)
			continue
		}
		seen[name]++
		if k.operator {
			operators[name] = true
		}
	}
	for _, name := range fieldOf {
		if seen[name] != 1 {
			t.Errorf("Config.%s is listed %d times by namedKeys, want exactly once; a key the separation rule "+
				"does not list is a key it never compares", name, seen[name])
		}
	}
	if len(operators) != 2 || !operators["OperatorTOTPKEK"] || !operators["OperatorTokenHMACKey"] {
		t.Errorf("namedKeys marks %v as operator keys, want exactly OperatorTOTPKEK and OperatorTokenHMACKey", operators)
	}
}
