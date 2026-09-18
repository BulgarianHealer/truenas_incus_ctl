package cmd

import (
	"reflect"
	"testing"
)

// TrueNAS 26 отказва `pool.dataset.rename` без `force` с EINVAL:
// „No safety checks are performed when renaming ZFS resources".
// Старото `zfs.dataset.rename` нямаше такова поле, затова преводът трябва да го добави.
func TestDatasetRenameAddsForce(t *testing.T) {
	params := []interface{}{
		"tank/incus/containers/x",
		map[string]interface{}{"new_name": "tank/incus/containers/y"},
	}

	method, got, ok := translateForNewApi("zfs.dataset.rename", params)

	if !ok {
		t.Fatal("преводът не е разпознат")
	}
	if method != "pool.dataset.rename" {
		t.Fatalf("метод = %q, очакван pool.dataset.rename", method)
	}

	arr := got.([]interface{})
	opts := arr[1].(map[string]interface{})

	if opts["force"] != true {
		t.Fatalf("force = %v, очакван true", opts["force"])
	}
	if opts["new_name"] != "tank/incus/containers/y" {
		t.Fatalf("new_name е загубен: %v", opts["new_name"])
	}
}

// Изрично подадена стойност не се презаписва — извикващият има последната дума.
func TestDatasetRenameKeepsExplicitForce(t *testing.T) {
	params := []interface{}{
		"tank/x",
		map[string]interface{}{"new_name": "tank/y", "force": false},
	}

	_, got, _ := translateForNewApi("zfs.dataset.rename", params)
	opts := got.([]interface{})[1].(map[string]interface{})

	if opts["force"] != false {
		t.Fatalf("force = %v, очакван false (подаден изрично)", opts["force"])
	}
}

// Неочаквана форма на params не бива да гърми, нито да променя нищо.
func TestSetKeyInSecondObjectLeavesOtherShapesAlone(t *testing.T) {
	cases := []interface{}{
		nil,
		"низ",
		[]interface{}{},
		[]interface{}{"само един елемент"},
		[]interface{}{"път", "втори не е map"},
	}

	for _, in := range cases {
		out := setKeyInSecondObject(in, "force", true)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("формата е променена: %#v → %#v", in, out)
		}
	}
}
