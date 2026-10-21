package main

import (
	"slices"
	"testing"
)

func TestParseKube(t *testing.T) {
	t.Parallel()

	cases := []struct {
		arg      string
		resource string
		opts     []string
		bad      bool
	}{
		{arg: "k8s://secrets/alertmanager-main", resource: "secrets/alertmanager-main"},
		{arg: "k8s://deploy/api?namespace=prod", resource: "deploy/api", opts: []string{"-n", "prod"}},
		{arg: "k8s://deploy/api?ns=prod", resource: "deploy/api", opts: []string{"-n", "prod"}},
		{arg: "k8s://deploy/api?context=staging", resource: "deploy/api", opts: []string{"--context", "staging"}},
		{arg: "k8s://deploy?kubeconfig=/tmp/kc", resource: "deploy", opts: []string{"--kubeconfig", "/tmp/kc"}},
		{arg: "k8s://", bad: true},
		{arg: "k8s://deploy/api?container=x", bad: true},
		{arg: "kubectl://pod/etc/hosts", bad: true},
	}
	for _, c := range cases {
		got, err := parseKube(c.arg)
		if c.bad {
			if err == nil {
				t.Errorf("parseKube(%q) accepted %+v", c.arg, got)
			}

			continue
		}

		if err != nil || got.resource != c.resource || !slices.Equal(got.opts, c.opts) {
			t.Errorf("parseKube(%q) = %+v, %v, want %s %v", c.arg, got, err, c.resource, c.opts)
		}
	}
}
