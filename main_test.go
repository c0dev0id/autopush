package main

import (
	"reflect"
	"testing"
)

func TestExpandBundledFlags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"single bool untouched", []string{"prog", "-r"}, []string{"prog", "-r"}},
		{"bundle arp", []string{"prog", "-arp"}, []string{"prog", "-a", "-r", "-p"}},
		{"bundle with interval", []string{"prog", "-rap60"}, []string{"prog", "-r", "-a", "-p=60"}},
		{"oneshot bundle", []string{"prog", "-1a"}, []string{"prog", "-1", "-a"}},
		{"p not last, no digits", []string{"prog", "-par"}, []string{"prog", "-p", "-a", "-r"}},
		{"equals form untouched", []string{"prog", "-p=30"}, []string{"prog", "-p=30"}},
		{"long flag untouched", []string{"prog", "--help"}, []string{"prog", "--help"}},
		{"unknown char passes through", []string{"prog", "-xyz"}, []string{"prog", "-xyz"}},
		{"misplaced digit passes through", []string{"prog", "-p6r"}, []string{"prog", "-p6r"}},
		{"path arg untouched", []string{"prog", "-r", "some/path"}, []string{"prog", "-r", "some/path"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandBundledFlags(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("expandBundledFlags(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
